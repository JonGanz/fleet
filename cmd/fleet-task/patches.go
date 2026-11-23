package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/xdg"
)

// patchInfo describes one patch file available for a repo, as reported by
// `fleet-task patches` and `fleet-task repos`.
type patchInfo struct {
	Repo    string   `json:"repo"`
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Summary string   `json:"summary"`
	Files   []string `json:"files"`
}

// maxPatchSize caps how much of a patch file is read for its summary and
// file list; patches are small hand-made tweaks, so anything larger is
// almost certainly not one.
const maxPatchSize = 16 << 20

// describePatch reads the patch at path and returns its patchInfo.
func describePatch(repo, path string) (patchInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return patchInfo{}, err
	}
	if info.Size() > maxPatchSize {
		return patchInfo{}, fmt.Errorf("patch %s is larger than %d bytes", path, maxPatchSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return patchInfo{}, err
	}
	content := string(data)
	return patchInfo{
		Repo:    repo,
		Name:    filepath.Base(path),
		Path:    path,
		Summary: parsePatchSummary(content),
		Files:   parsePatchFiles(content),
	}, nil
}

// isFormatPatchHeader reports whether line is one of the mail headers
// `git format-patch` writes above the subject.
func isFormatPatchHeader(line string) bool {
	for _, prefix := range []string{"From ", "From: ", "Date: "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// isPatchBodyStart reports whether line begins the diff itself, ending the
// free-text header that `git apply` ignores.
func isPatchBodyStart(line string) bool {
	return strings.HasPrefix(line, "diff --git ") ||
		strings.HasPrefix(line, "--- ") ||
		strings.HasPrefix(line, "Index: ")
}

// listPatchPaths returns the sorted paths of <config dir>/patches/<repo>/*.patch.
// A missing patches dir yields no patches, not an error.
func listPatchPaths(repo string) ([]string, error) {
	dir, err := xdg.PatchesDir(repo)
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.patch"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// listPatches describes every patch available for repo. A patch that can't
// be read is reported with a warning and skipped rather than failing the
// whole listing.
func listPatches(repo string) ([]patchInfo, error) {
	paths, err := listPatchPaths(repo)
	if err != nil {
		return nil, err
	}
	infos := make([]patchInfo, 0, len(paths))
	for _, p := range paths {
		info, err := describePatch(repo, p)
		if err != nil {
			cli.Warnf("reading patch %s: %v", p, err)
			continue
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// parsePatchFiles returns the paths a patch touches, in order of first
// appearance, from its `diff --git a/<x> b/<y>` headers (the b/ side, so a
// rename reports its destination). Plain unified diffs without git headers
// fall back to `+++ b/<path>` lines. /dev/null (file deletion) is skipped.
func parsePatchFiles(content string) []string {
	seen := map[string]bool{}
	files := []string{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || p == "/dev/null" || seen[p] {
			return
		}
		seen[p] = true
		files = append(files, p)
	}

	hasGitHeaders := strings.Contains(content, "\ndiff --git ") || strings.HasPrefix(content, "diff --git ")
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), maxPatchSize)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case hasGitHeaders && strings.HasPrefix(line, "diff --git "):
			if i := strings.LastIndex(line, " b/"); i >= 0 {
				add(line[i+len(" b/"):])
			}
		case !hasGitHeaders && strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			if tab := strings.IndexByte(p, '\t'); tab >= 0 {
				p = p[:tab] // drop a trailing timestamp
			}
			add(strings.TrimPrefix(p, "b/"))
		}
	}
	return files
}

// parsePatchSummary returns a one-line description of a patch: the
// `Subject:` header of a `git format-patch` file (minus any "[PATCH n/m]"
// prefix), else the first non-blank line of free text before the diff
// starts, else "".
func parsePatchSummary(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), maxPatchSize)
	firstText := ""
	for scanner.Scan() {
		line := scanner.Text()
		if isPatchBodyStart(line) {
			break
		}
		if subject, ok := strings.CutPrefix(line, "Subject: "); ok {
			return stripPatchPrefix(strings.TrimSpace(subject))
		}
		trimmed := strings.TrimSpace(line)
		if firstText == "" && trimmed != "" && !isFormatPatchHeader(trimmed) {
			firstText = trimmed
		}
	}
	return firstText
}

// resolvePatches decides which patch files to apply for each repo in
// repoNames when patches are chosen by flags instead of the picker: every
// available patch, minus those named in skip ("<repo>:<name>"), or none at
// all if noPatches is set. It is a usage error to skip a patch that doesn't
// exist or belongs to a repo not in repoNames, so typos fail before any
// worktree is touched. available maps repo name to its patch paths.
func resolvePatches(repoNames []string, available map[string][]string, skip []string, noPatches bool) (map[string][]string, error) {
	inScope := make(map[string]bool, len(repoNames))
	for _, r := range repoNames {
		inScope[r] = true
	}

	skipSet := map[string]bool{}
	for _, s := range skip {
		repo, name, ok := strings.Cut(s, ":")
		if !ok || repo == "" || name == "" {
			return nil, cli.Usagef("--skip-patch %q must be <repo>:<patch-name>", s)
		}
		if !inScope[repo] {
			return nil, cli.Usagef("--skip-patch %q: repo %s is not being added", s, repo)
		}
		found := false
		for _, p := range available[repo] {
			if filepath.Base(p) == name {
				found = true
				break
			}
		}
		if !found {
			return nil, cli.Usagef("--skip-patch %q: no patch %s for repo %s (see `fleet-task patches %s`)", s, name, repo, repo)
		}
		skipSet[repo+":"+name] = true
	}

	chosen := make(map[string][]string, len(repoNames))
	for _, repo := range repoNames {
		selected := []string{}
		if !noPatches {
			for _, p := range available[repo] {
				if !skipSet[repo+":"+filepath.Base(p)] {
					selected = append(selected, p)
				}
			}
		}
		chosen[repo] = selected
	}
	return chosen, nil
}

// stripPatchPrefix removes a leading "[PATCH]" / "[PATCH 1/3]" tag from a
// format-patch subject.
func stripPatchPrefix(subject string) string {
	if strings.HasPrefix(subject, "[") {
		if end := strings.Index(subject, "]"); end >= 0 && strings.Contains(strings.ToUpper(subject[:end]), "PATCH") {
			return strings.TrimSpace(subject[end+1:])
		}
	}
	return subject
}
