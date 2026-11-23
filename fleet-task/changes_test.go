package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// changesFixture creates a task T-1 with repo r whose worktree has
// local.patch (editing a.txt) applied, and returns the task's only repo
// entry and the patch file path.
func changesFixture(t *testing.T) (TaskRepo, string) {
	t.Helper()
	requireGit(t)
	origin := makeOrigin(t)
	writeFileT(t, filepath.Join(origin, "a.txt"), "one\ntwo\nthree\n")
	writeFileT(t, filepath.Join(origin, "b.txt"), "bee\n")
	writeFileT(t, filepath.Join(origin, ".gitignore"), "*.log\n")
	gitT(t, origin, "add", ".")
	gitT(t, origin, "commit", "-q", "-m", "files")

	// The patch: a plain `git diff` of an edit to a.txt.
	writeFileT(t, filepath.Join(origin, "a.txt"), "one\nTWO (patched)\nthree\n")
	patch := gitT(t, origin, "diff") + "\n"
	gitT(t, origin, "checkout", "--", "a.txt")

	cfgDir := fleetEnv(t, `
defaults:
  default_branch: main
  base_root: `+t.TempDir()+`
repos:
  - name: r
    origin: `+origin+`
`)
	patchPath := filepath.Join(cfgDir, "patches", "r", "local.patch")
	writeFileT(t, patchPath, patch)

	if err := cmdNew(newOpts{Ticket: "T-1", Repos: []string{"r"}}); err != nil {
		t.Fatalf("new: %v", err)
	}
	tf, _ := taskFile("T-1")
	st, err := readTaskState(tf)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Repos) != 1 || !reflect.DeepEqual(st.Repos[0].Patches, []string{"local.patch"}) {
		t.Fatalf("fixture: patches not recorded: %+v", st.Repos)
	}
	return st.Repos[0], patchPath
}

func changesOf(t *testing.T, r TaskRepo) changeReport {
	t.Helper()
	return inspectRepoStatus(nil, r).Changes
}

func TestChangesExcludePatches(t *testing.T) {
	r, _ := changesFixture(t)
	wt := r.WorktreePath

	rep := changesOf(t, r)
	if rep.State != changesClean || !rep.PatchesExcluded || rep.Approximate {
		t.Fatalf("freshly patched worktree: %+v", rep)
	}

	// Ignored file: still clean.
	writeFileT(t, filepath.Join(wt, "debug.log"), "noise")
	if rep := changesOf(t, r); rep.State != changesClean {
		t.Errorf("ignored file: %+v", rep)
	}

	// Edit in an unpatched file: only that file.
	writeFileT(t, filepath.Join(wt, "b.txt"), "bee edited\n")
	rep = changesOf(t, r)
	if want := []fileChange{{"M", "b.txt"}}; rep.State != changesDirty || !reflect.DeepEqual(rep.Files, want) {
		t.Errorf("unpatched edit: %+v", rep)
	}
	gitT(t, wt, "checkout", "--", "b.txt")

	// Edit inside the patched file, on top of the patch.
	writeFileT(t, filepath.Join(wt, "a.txt"), "one\nTWO (patched)\nthree\nfour\n")
	rep = changesOf(t, r)
	if want := []fileChange{{"M", "a.txt"}}; rep.State != changesDirty || !reflect.DeepEqual(rep.Files, want) {
		t.Errorf("edit in patched file: %+v", rep)
	}
	writeFileT(t, filepath.Join(wt, "a.txt"), "one\nTWO (patched)\nthree\n")

	// Untracked file.
	writeFileT(t, filepath.Join(wt, "new.txt"), "hi")
	rep = changesOf(t, r)
	if want := []fileChange{{"A", "new.txt"}}; rep.State != changesDirty || !reflect.DeepEqual(rep.Files, want) {
		t.Errorf("untracked: %+v", rep)
	}
	os.Remove(filepath.Join(wt, "new.txt"))

	// The real index must be untouched by all of the above.
	if staged := gitT(t, wt, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("real index modified: %q", staged)
	}
}

func TestChangesPatchFileDeletedFallsBack(t *testing.T) {
	r, patchPath := changesFixture(t)
	if err := os.Remove(patchPath); err != nil {
		t.Fatal(err)
	}
	rep := changesOf(t, r)
	if rep.PatchesExcluded || rep.Reason == "" {
		t.Errorf("want patches_excluded=false with a reason: %+v", rep)
	}
	if want := []fileChange{{"M", "a.txt"}}; rep.State != changesDirty || !reflect.DeepEqual(rep.Files, want) {
		t.Errorf("patch changes should now show: %+v", rep)
	}
}

func TestChangesPatchCommittedOver(t *testing.T) {
	r, _ := changesFixture(t)
	gitT(t, r.WorktreePath, "commit", "-q", "-am", "keep the patch")
	st := inspectRepoStatus(nil, r)
	if st.Changes.State != changesClean || st.Changes.PatchesExcluded || !strings.Contains(st.Changes.Reason, "no longer applies") {
		t.Errorf("committed patch: %+v", st.Changes)
	}
	if st.Unpushed == nil || *st.Unpushed != 1 {
		t.Errorf("unpushed = %v, want 1", st.Unpushed)
	}
}

func TestChangesUnknownPatchesAndMissingWorktree(t *testing.T) {
	r, _ := changesFixture(t)
	r.Patches = nil
	rep := changesOf(t, r)
	if rep.PatchesExcluded || rep.State != changesDirty || !strings.Contains(rep.Reason, "unknown") {
		t.Errorf("null patches: %+v", rep)
	}

	r.WorktreePath = filepath.Join(t.TempDir(), "gone")
	if rep := changesOf(t, r); rep.State != changesUnknown || rep.Reason == "" {
		t.Errorf("missing worktree: %+v", rep)
	}
}

func TestRmRefusesDirtyWithoutTerminal(t *testing.T) {
	r, _ := changesFixture(t)
	writeFileT(t, filepath.Join(r.WorktreePath, "b.txt"), "unsaved work\n")
	tf, _ := taskFile("T-1")

	err := cmdRm("T-1", false, true)
	if err == nil || !isUsageError(err) || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want usage error naming --force, got %v", err)
	}
	if _, err := os.Stat(r.WorktreePath); err != nil {
		t.Errorf("worktree removed despite refusal: %v", err)
	}
	if _, err := os.Stat(tf); err != nil {
		t.Errorf("state removed despite refusal: %v", err)
	}

	// edit --remove is guarded the same way.
	err = cmdEdit(editOpts{Ticket: "T-1", Remove: []string{"r"}})
	if err == nil || !isUsageError(err) {
		t.Errorf("edit --remove: want usage error, got %v", err)
	}

	if err := cmdRm("T-1", true, false); err != nil {
		t.Fatalf("rm --force: %v", err)
	}
	if _, err := os.Stat(r.WorktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree still there after --force: %v", err)
	}
}

func TestRmCleanWorktreeNeedsNoForce(t *testing.T) {
	r, _ := changesFixture(t)
	if err := cmdRm("T-1", false, false); err != nil {
		t.Fatalf("rm of a clean (patched) worktree: %v", err)
	}
	if _, err := os.Stat(r.WorktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree still there: %v", err)
	}
}

func TestParseNameStatus(t *testing.T) {
	got := parseNameStatus("M\x00a.txt\x00A\x00dir/new file\x00R100\x00old\x00new\x00")
	want := []fileChange{{"M", "a.txt"}, {"A", "dir/new file"}, {"R", "new"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNameStatus = %v", got)
	}
	if got := parseNameStatus(""); len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
}

func TestParsePorcelainLines(t *testing.T) {
	got := parsePorcelainLines(" M a.txt\r\n?? new.txt\nM  staged.txt\nR  old.txt -> renamed.txt\n?? \"sp ace\\303\\251.txt\"\n")
	want := []fileChange{{"M", "a.txt"}, {"A", "new.txt"}, {"M", "staged.txt"}, {"R", "renamed.txt"}, {"A", "sp aceé.txt"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePorcelainLines = %v\nwant %v", got, want)
	}
}

func TestSubtractPatchedFiles(t *testing.T) {
	patch := filepath.Join(t.TempDir(), "p.patch")
	writeFileT(t, patch, "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-x\n+y\n")
	files := []fileChange{{"M", "a.txt"}, {"M", "b.txt"}}
	kept, ok := subtractPatchedFiles(files, []string{patch})
	if !ok || !reflect.DeepEqual(kept, []fileChange{{"M", "b.txt"}}) {
		t.Errorf("kept = %v, ok = %v", kept, ok)
	}
	if kept, ok := subtractPatchedFiles(files, []string{patch + ".missing"}); ok || len(kept) != 2 {
		t.Errorf("unreadable patch must subtract nothing: %v, %v", kept, ok)
	}
}

func TestDescribeChangeProblemCaps(t *testing.T) {
	var files []fileChange
	for i := 0; i < maxListedChanges+5; i++ {
		files = append(files, fileChange{"M", "f" + string(rune('a'+i))})
	}
	s := describeChangeProblem("r", changeReport{State: changesDirty, Files: files, PatchesExcluded: true})
	if !strings.Contains(s, "and 5 more") || strings.Count(s, "\n") != maxListedChanges+1 {
		t.Errorf("not capped:\n%s", s)
	}
	if s := describeChangeProblem("r", unknownChanges("boom")); !strings.Contains(s, "could not check") {
		t.Errorf("unknown: %s", s)
	}
}

func TestFormatters(t *testing.T) {
	if formatChanges(changeReport{State: changesDirty, Files: make([]fileChange, 3)}) != "3 changed" {
		t.Error("dirty")
	}
	if formatChanges(changeReport{State: changesDirty, Files: make([]fileChange, 1), Approximate: true}) != "~1 changed" {
		t.Error("approximate")
	}
	n := 2
	if formatUnpushed(nil) != "?" || formatUnpushed(&n) != "2" {
		t.Error("unpushed")
	}
	if formatRepoChangeCounts(0, 0) != "clean" || formatRepoChangeCounts(1, 2) != "1 dirty, 2 unknown" {
		t.Error("counts")
	}
}
