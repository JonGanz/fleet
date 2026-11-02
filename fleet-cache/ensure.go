package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// markerPath returns the path of the marker file `ensure` writes into a
// target node_modules recording which cache hash last populated it.
func markerPath(targetNodeModules string) string {
	return filepath.Join(targetNodeModules, ".fleet-cache-hash")
}

// alreadyLinked reports whether targetNodeModules's marker matches hash --
// i.e. a previous `ensure` call already populated it from this exact cache
// entry, so it's safe to skip re-linking and leave whatever the user has
// since added (e.g. an `npm link` symlink) untouched.
func alreadyLinked(targetNodeModules, hash string) bool {
	data, err := os.ReadFile(markerPath(targetNodeModules))
	return err == nil && strings.TrimSpace(string(data)) == hash
}

// writeMarker records hash as the cache entry that populated
// targetNodeModules, so a future `ensure` call can detect nothing's changed
// and skip the destructive rebuild.
func writeMarker(targetNodeModules, hash string) error {
	return os.WriteFile(markerPath(targetNodeModules), []byte(hash), 0o644)
}

// nearestExisting returns path or its closest existing ancestor.
func nearestExisting(path string) string {
	p, err := filepath.Abs(path)
	if err != nil {
		p = path
	}
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// checkSameDevice errors clearly if the cache root and the target dir are on
// different filesystems, before any expensive `npm ci` runs: hardlinks can't
// cross devices (EXDEV).
func checkSameDevice(root, dir string) error {
	same, err := sameDevice(nearestExisting(root), dir)
	if err != nil {
		return nil // can't tell; the per-file EXDEV check still applies
	}
	if !same {
		return fmt.Errorf("cache root %s and %s are on different filesystems, so node_modules can't be hardlinked; point --cache-root (or $FLEET_CACHE_DIR) at the same filesystem as the worktree", root, dir)
	}
	return nil
}

// runEnsure implements `fleet-cache ensure <dir> [--force] [--cache-root <dir>]`.
//
// This is Linux/WSL2-only. Windows-runtime repos (per repos.yaml's
// `runtime: windows` field, see docs/CONTRACT.md) use the separate
// `ensure-windows` command (ensure_win.go) instead, since hardlinks
// can't cross the WSL9P boundary.
func runEnsure(dir string, force bool, cacheRootFlag string) error {
	lockPath, hash, err := lockfileHash(dir)
	if err != nil {
		return err
	}
	targetNodeModules := filepath.Join(dir, "node_modules")

	// If this exact cache hash already populated the target, skip the
	// destructive rebuild entirely -- node_modules is otherwise treated as a
	// disposable derived artifact and blown away/relinked on every call,
	// which silently destroys anything the user has since added by hand
	// (most notably `npm link`'s package symlinks).
	if !force && alreadyLinked(targetNodeModules, hash) {
		logf("node_modules already linked to cache %s, nothing to do", shortHash(hash))
		return nil
	}

	root, err := cacheRoot(cacheRootFlag)
	if err != nil {
		return fmt.Errorf("resolving cache dir: %w", err)
	}
	if err := checkSameDevice(root, dir); err != nil {
		return err
	}

	entry := filepath.Join(nodeCacheDir(root), hash)
	err = ensureCacheEntry(entry, hash, lockPath, entryOps{
		install: func(staging string) error {
			cmd := exec.Command("npm", "ci")
			cmd.Dir = staging
			// npm's output goes to stderr too: stdout is reserved for
			// machine-readable output and must not interleave with callers'.
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("npm ci failed in %s: %w", staging, err)
			}
			return nil
		},
		finalize: func(staging string) error {
			if err := lockDownPermissions(filepath.Join(staging, "node_modules")); err != nil {
				return fmt.Errorf("locking down cache entry permissions: %w", err)
			}
			return nil
		},
		remove: os.RemoveAll,
	})
	if err != nil {
		return err
	}

	if _, err := os.Stat(targetNodeModules); err == nil {
		if err := os.RemoveAll(targetNodeModules); err != nil {
			return fmt.Errorf("removing existing %s: %w", targetNodeModules, err)
		}
	}

	if err := hardlinkTree(filepath.Join(entry, "node_modules"), targetNodeModules); err != nil {
		return fmt.Errorf("hardlinking node_modules into %s: %w", dir, err)
	}

	if err := writeMarker(targetNodeModules, hash); err != nil {
		logf("warning: writing cache marker: %v", err)
	}

	logf("linked node_modules from cache %s", shortHash(hash))
	return nil
}
