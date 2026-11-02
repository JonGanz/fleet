package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// referencedHashes walks each root directory recursively looking for
// package-lock.json files, hashes each one found, and returns the set of
// hashes still "referenced" by something on disk.
//
// Every root must exist and be a readable directory, and any unreadable
// path under it is an error: silently skipping them would leave lockfiles
// unseen, and gc --force would then delete cache entries that are still in
// use (e.g. a typo'd root deleting the whole cache). Symlinked directories
// (including a symlinked root, and the WSL-side symlinks fleet-task creates
// for windows-runtime worktrees) are followed once each.
func referencedHashes(roots []string) (map[string]bool, error) {
	referenced := make(map[string]bool)
	visited := make(map[string]bool)
	for _, root := range roots {
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("--roots %s: %w", root, err)
		}
		fi, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("--roots %s: %w", root, err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("--roots %s: not a directory", root)
		}
		if err := walkLockfiles(real, visited, referenced); err != nil {
			return nil, err
		}
	}
	return referenced, nil
}

func walkLockfiles(dir string, visited, referenced map[string]bool) error {
	if visited[dir] {
		return nil
	}
	visited[dir] = true
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // vanished mid-scan
			}
			return fmt.Errorf("scanning %s: %w", path, err)
		}
		if info.IsDir() && info.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if info.Name() == "node_modules" {
				return nil
			}
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil // dangling symlink
			}
			target, err := os.Stat(real)
			if err != nil {
				return nil
			}
			if target.IsDir() {
				return walkLockfiles(real, visited, referenced)
			}
			info = target
		}
		if !info.IsDir() && info.Name() == "package-lock.json" {
			hash, err := hashFile(path)
			if err != nil {
				return fmt.Errorf("hashing %s: %w", path, err)
			}
			referenced[hash] = true
		}
		return nil
	})
}

// unreferencedEntries lists the cache entry hashes present under
// <node-cache dir> that are not in the referenced set. Staging dirs
// (<hash>.tmp-<pid>) are not entries and are handled by staleStagingDirs.
func unreferencedEntries(nodeCache string, referenced map[string]bool) ([]string, error) {
	items, err := os.ReadDir(nodeCache)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var stale []string
	for _, item := range items {
		if !item.IsDir() || isStagingName(item.Name()) {
			continue
		}
		if !referenced[item.Name()] {
			stale = append(stale, item.Name())
		}
	}
	return stale, nil
}

// staleStagingDirs lists staging dirs under nodeCache last modified more
// than maxAge before now -- leftovers of a killed `ensure`. Younger ones may
// belong to a live `ensure` and are never flagged.
func staleStagingDirs(nodeCache string, now time.Time, maxAge time.Duration) ([]string, error) {
	items, err := os.ReadDir(nodeCache)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var stale []string
	for _, item := range items {
		if !item.IsDir() || !isStagingName(item.Name()) {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			stale = append(stale, item.Name())
		}
	}
	return stale, nil
}

// gcItem is one stale thing gc found.
type gcItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Kind    string `json:"kind"` // "entry" or "staging"
	Removed bool   `json:"removed"`
}

// gcReport is gc's result, also its --json shape.
type gcReport struct {
	NodeCache string   `json:"node_cache"`
	DryRun    bool     `json:"dry_run"`
	Stale     []gcItem `json:"stale"`
}

// gcStale is the platform-independent gc core shared by gc and gc-windows.
// nodeCache is the WSL/Linux-visible node-cache dir. It finds stale entries
// (not referenced under roots) and abandoned staging dirs, and -- unless
// this is a dry run (no roots, or force unset) -- deletes them via remove.
// display maps a WSL/Linux path to the path to show the user (identity on
// Linux, win32 form for gc-windows). On a removal error the partial report
// is returned along with the error.
func gcStale(nodeCache string, roots []string, force bool, now time.Time,
	remove func(path string) error, display func(path string) (string, error)) (*gcReport, error) {

	referenced, err := referencedHashes(roots)
	if err != nil {
		return nil, err
	}
	entries, err := unreferencedEntries(nodeCache, referenced)
	if err != nil {
		return nil, err
	}
	staging, err := staleStagingDirs(nodeCache, now, stagingMaxAge)
	if err != nil {
		return nil, err
	}

	report := &gcReport{NodeCache: nodeCache, DryRun: len(roots) == 0 || !force, Stale: []gcItem{}}
	add := func(names []string, kind string) error {
		sort.Strings(names)
		for _, name := range names {
			wslPath := filepath.Join(nodeCache, name)
			shown, err := display(wslPath)
			if err != nil {
				return fmt.Errorf("resolving %s: %w", wslPath, err)
			}
			report.Stale = append(report.Stale, gcItem{Name: name, Path: shown, Kind: kind})
		}
		return nil
	}
	if err := add(entries, "entry"); err != nil {
		return nil, err
	}
	if err := add(staging, "staging"); err != nil {
		return nil, err
	}

	if report.DryRun {
		return report, nil
	}
	for i := range report.Stale {
		wslPath := filepath.Join(nodeCache, report.Stale[i].Name)
		if err := remove(wslPath); err != nil {
			return report, fmt.Errorf("removing %s: %w", report.Stale[i].Path, err)
		}
		report.Stale[i].Removed = true
	}
	return report, nil
}

// printGCReport writes the report to w: JSON if asJSON, else one
// "would remove"/"removed" line per item. Result lines are stdout (they are
// gc's output); hints go to stderr via logf.
func printGCReport(w io.Writer, r *gcReport, asJSON bool, label string, hadRoots bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	if len(r.Stale) == 0 {
		fmt.Fprintf(w, "no stale %scache entries found\n", label)
		return nil
	}
	for _, it := range r.Stale {
		verb := "would remove"
		if it.Removed {
			verb = "removed"
		}
		suffix := ""
		if it.Kind == "staging" {
			suffix = " (abandoned staging dir)"
		}
		fmt.Fprintf(w, "%s %s%s\n", verb, it.Path, suffix)
	}
	if r.DryRun {
		if !hadRoots {
			logf("no --roots given: nothing is considered referenced; pass --roots and --force to actually delete")
		} else {
			logf("dry run: pass --force to actually delete")
		}
	}
	return nil
}

// runGCCommon runs gcStale and prints the report (even a partial one on a
// removal error).
func runGCCommon(nodeCache, label string, roots []string, force, asJSON bool,
	remove func(string) error, display func(string) (string, error)) error {

	report, err := gcStale(nodeCache, roots, force, time.Now(), remove, display)
	if report != nil {
		if perr := printGCReport(os.Stdout, report, asJSON, label, len(roots) > 0); perr != nil && err == nil {
			err = perr
		}
	}
	return err
}

func runGC(roots []string, force, asJSON bool, cacheRootFlag string) error {
	root, err := cacheRoot(cacheRootFlag)
	if err != nil {
		return err
	}
	return runGCCommon(nodeCacheDir(root), "", roots, force, asJSON,
		os.RemoveAll,
		func(p string) (string, error) { return p, nil })
}
