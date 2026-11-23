package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/taskstate"
	"github.com/JonGanz/fleet/internal/xdg"
)

// Change states reported for a worktree.
const (
	changesClean   = "clean"
	changesDirty   = "dirty"
	changesUnknown = "unknown"
)

// maxListedChanges caps how many changed files a removal warning lists per
// repo; the rest are summarised as a count.
const maxListedChanges = 10

// changeReport describes a worktree's uncommitted changes beyond the
// patches fleet-task itself applied (which `git apply` leaves uncommitted by
// design, so a plain `git status` would flag nearly every worktree).
type changeReport struct {
	State string       `json:"state"` // clean | dirty | unknown
	Files []fileChange `json:"files"`
	// PatchesExcluded: the recorded patches' changes were subtracted.
	// False when there was nothing to subtract reliably (unknown patch
	// list, patch file gone, patch no longer applies to HEAD).
	PatchesExcluded bool `json:"patches_excluded"`
	// Approximate: the result is not exact (windows runtime: files the
	// patches touch are skipped entirely rather than compared).
	Approximate bool   `json:"approximate"`
	Reason      string `json:"reason,omitempty"`
}

// fileChange is one changed path; Status is git's letter (A, M, D, T, ...).
type fileChange struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

// repoStatus is everything `show` and the removal guard report per repo.
type repoStatus struct {
	Changes changeReport
	// Unpushed is the number of commits on the branch that are on no
	// remote, or nil if it couldn't be determined. Informational only: the
	// branch survives `rm`, so these commits are not lost by it.
	Unpushed *int
}

// checkRemovable is the guard `rm` and `edit --remove` run over every repo
// they are about to remove, before removing any. Clean worktrees, and ones
// whose directory is already gone (nothing left to lose), pass silently.
// Otherwise the problems are listed on stderr and the user must confirm on
// a terminal; without one it is a usage error naming --force. force skips
// the check entirely.
func checkRemovable(cfg *config.ReposConfig, repos []taskstate.Repo, force bool) error {
	if force {
		return nil
	}
	var problems []string
	for _, r := range repos {
		if _, err := os.Stat(r.WorktreePath); os.IsNotExist(err) {
			continue
		}
		rep := inspectRepoStatus(cfg, r).Changes
		if rep.State == changesClean {
			continue
		}
		problems = append(problems, describeChangeProblem(r.Repo, rep))
	}
	if len(problems) == 0 {
		return nil
	}
	for _, p := range problems {
		cli.Warnf("%s", p)
	}
	if !cli.InteractiveAllowed() {
		return cli.Usagef("%d repo(s) have uncommitted changes or could not be checked (see warnings above); nothing was removed -- commit or discard them first, or pass --force to remove anyway", len(problems))
	}
	ok, err := confirm("Remove anyway, losing these changes? [y/N] ")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("aborted; nothing was removed")
	}
	return nil
}

// confirm asks a yes/no question on stderr and reads the answer from
// stdin (only called when stdin is a terminal). Anything but y/yes is no.
func confirm(prompt string) (bool, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// describeChangeProblem renders one repo's non-clean report for the removal
// warning, listing at most maxListedChanges files.
func describeChangeProblem(repo string, rep changeReport) string {
	var b strings.Builder
	if rep.State == changesUnknown {
		fmt.Fprintf(&b, "%s: could not check for uncommitted changes: %s", repo, rep.Reason)
		return b.String()
	}
	fmt.Fprintf(&b, "%s has %d uncommitted change(s)", repo, len(rep.Files))
	switch {
	case !rep.PatchesExcluded && rep.Reason != "":
		fmt.Fprintf(&b, " (%s)", rep.Reason)
	case rep.Approximate:
		b.WriteString(" (approximate: files touched by applied patches were skipped)")
	}
	b.WriteString(":")
	for i, f := range rep.Files {
		if i == maxListedChanges {
			fmt.Fprintf(&b, "\n    ... and %d more", len(rep.Files)-maxListedChanges)
			break
		}
		fmt.Fprintf(&b, "\n    %s %s", f.Status, f.Path)
	}
	return b.String()
}

// formatChanges renders a report for the `show` table.
func formatChanges(rep changeReport) string {
	switch rep.State {
	case changesClean:
		return changesClean
	case changesDirty:
		s := fmt.Sprintf("%d changed", len(rep.Files))
		if rep.Approximate {
			s = "~" + s
		}
		return s
	default:
		return changesUnknown
	}
}

// formatUnpushed renders an unpushed-commit count for the `show` table.
func formatUnpushed(n *int) string {
	if n == nil {
		return "?"
	}
	return fmt.Sprint(*n)
}

// inspectRepoStatus computes one task repo's change report and unpushed
// count. Commands run inside the worktree itself, so no base is needed;
// the runtime still matters (a windows worktree's .git points at a win32
// path only git.exe understands). A repo missing from repos.yaml is
// treated as linux: if it was really windows, git fails and the result is
// "unknown", never "clean".
func inspectRepoStatus(cfg *config.ReposConfig, r taskstate.Repo) repoStatus {
	var runner gitRunner = linuxGit{}
	if repo := cfg.FindRepo(r.Repo); repo != nil && repo.Runtime == "windows" {
		runner = windowsGit{cfg: cfg}
	}

	var st repoStatus
	if _, err := os.Stat(r.WorktreePath); err != nil {
		st.Changes = unknownChanges(fmt.Sprintf("worktree %s is missing (%v)", r.WorktreePath, err))
		return st
	}

	patchPaths, problem := resolveRecordedPatches(r.Repo, r.Patches)
	exclude := problem == ""
	rep := runner.Changes(r.WorktreePath, patchPaths, exclude)
	if !exclude && rep.State != changesUnknown {
		rep.PatchesExcluded = false
		rep.Reason = problem
	}
	st.Changes = rep

	if n, err := runner.UnpushedCommits(r.WorktreePath, r.Branch); err == nil {
		st.Unpushed = &n
	}
	return st
}

// parseNameStatus parses `git diff-tree -r --name-status -z` output:
// alternating status and path fields (a rename/copy status is followed by
// two paths; the destination is reported).
func parseNameStatus(data string) []fileChange {
	fields := strings.Split(strings.TrimRight(data, "\x00"), "\x00")
	files := []fileChange{}
	for i := 0; i+1 < len(fields); {
		status := fields[i]
		if status == "" {
			i++
			continue
		}
		path := fields[i+1]
		i += 2
		if (status[0] == 'R' || status[0] == 'C') && i < len(fields) {
			path = fields[i]
			i++
		}
		files = append(files, fileChange{Status: status[:1], Path: path})
	}
	return files
}

// parsePorcelainLines parses `git status --porcelain=v1` output (one
// "XY path" entry per line; not -z, whose NUL bytes may not survive
// PowerShell's handling of native output). Untracked files ("??") are
// reported as A; otherwise the worktree-side letter wins over the
// index-side one. A rename's "old -> new" reports new; C-quoted paths are
// unquoted.
func parsePorcelainLines(data string) []fileChange {
	files := []fileChange{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		x, y, path := line[0], line[1], line[3:]
		status := string(y)
		switch {
		case x == '?' && y == '?':
			status = "A"
		case y == ' ':
			status = string(x)
		}
		if (x == 'R' || x == 'C') && strings.Contains(path, " -> ") {
			path = path[strings.LastIndex(path, " -> ")+4:]
		}
		if strings.HasPrefix(path, `"`) {
			if unq, err := strconv.Unquote(path); err == nil {
				path = unq
			}
		}
		files = append(files, fileChange{Status: status, Path: path})
	}
	return files
}

// reportFromFiles builds a clean/dirty report from a list of changes.
func reportFromFiles(files []fileChange, patchesExcluded, approximate bool, reason string) changeReport {
	state := changesClean
	if len(files) > 0 {
		state = changesDirty
	}
	return changeReport{State: state, Files: files, PatchesExcluded: patchesExcluded, Approximate: approximate, Reason: reason}
}

// resolveRecordedPatches maps a state entry's applied patch names to files
// under <config dir>/patches/<repo>/. problem is non-empty when the patches
// can't be excluded: the list is unknown (null, older state file) or a file
// is gone. An empty list is fine: there is nothing to exclude.
func resolveRecordedPatches(repo string, names []string) (paths []string, problem string) {
	if names == nil {
		return nil, "applied patches unknown (task predates patch tracking), so their changes are included"
	}
	dir, err := xdg.PatchesDir(repo)
	if err != nil {
		return nil, fmt.Sprintf("patches dir: %v; patch changes are included", err)
	}
	for _, name := range names {
		if name == "" || filepath.Base(name) != name {
			return nil, fmt.Sprintf("recorded patch name %q is not a plain file name; patch changes are included", name)
		}
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Sprintf("recorded patch %s no longer exists, so patch changes are included", p)
		}
		paths = append(paths, p)
	}
	return paths, ""
}

// subtractPatchedFiles drops changes to files any of patchFiles touches
// (the windows-runtime approximation). It reports whether every patch could
// be read; if not, nothing is dropped.
func subtractPatchedFiles(files []fileChange, patchFiles []string) ([]fileChange, bool) {
	touched := map[string]bool{}
	for _, p := range patchFiles {
		data, err := os.ReadFile(p)
		if err != nil {
			return files, false
		}
		for _, f := range parsePatchFiles(string(data)) {
			touched[f] = true
		}
	}
	kept := []fileChange{}
	for _, f := range files {
		if !touched[f.Path] {
			kept = append(kept, f)
		}
	}
	return kept, true
}

// unknownChanges is a report for a worktree that couldn't be checked.
func unknownChanges(reason string) changeReport {
	return changeReport{State: changesUnknown, Files: []fileChange{}, Reason: reason}
}
