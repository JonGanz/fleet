package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/taskstate"
)

// fleetEnv sandboxes fleet-task's config/state dirs and tmux server for an
// end-to-end test and writes repos.yaml with the given body. Returns the
// config dir.
func fleetEnv(t *testing.T, reposYAML string) string {
	t.Helper()
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	t.Setenv("FLEET_CONFIG_DIR", cfgDir)
	t.Setenv("FLEET_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("FLEET_REPOS_FILE", "")
	t.Setenv("FLEET_TMUX_SOCKET", "fleet-task-test-none")
	t.Setenv("FLEET_NO_INPUT", "1")
	writeFileT(t, filepath.Join(cfgDir, "repos.yaml"), reposYAML)
	old := out
	out = os.Stderr
	t.Cleanup(func() { out = old })
	return cfgDir
}

func TestNewWritesStatePerRepo(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)
	bases := t.TempDir()
	cfgDir := fleetEnv(t, `
defaults:
  default_branch: main
  base_root: `+bases+`
repos:
  - name: a
    origin: `+origin+`
  - name: broken
    origin: `+filepath.Join(t.TempDir(), "no-such-origin")+`
  - name: b
    origin: `+origin+`
`)
	// A pre-create hook snapshots the state file and the hook env as each
	// repo starts, proving earlier repos were already recorded.
	snapDir := t.TempDir()
	hook := "#!/bin/sh\ncp \"$FLEET_STATE_DIR/tasks/$FLEET_TICKET.json\" \"" + snapDir + "/$FLEET_REPO.json\" 2>/dev/null\necho \"$FLEET_BRANCH|$FLEET_DESCRIPTION\" > \"" + snapDir + "/$FLEET_REPO.env\"\n"
	hookPath := filepath.Join(cfgDir, "hooks", "pre-create", "10-snap")
	writeFileT(t, hookPath, hook)
	if err := os.Chmod(hookPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := cmdNew(newOpts{Ticket: "T-1", Description: "Do it", Repos: []string{"a", "broken", "b"}, NoPatches: true})
	if err == nil || !strings.Contains(err.Error(), "1 of 3") {
		t.Fatalf("want partial failure, got %v", err)
	}

	snap, err := os.ReadFile(filepath.Join(snapDir, "b.json"))
	if err != nil || !strings.Contains(string(snap), `"repo": "a"`) {
		t.Errorf("state before b's setup should already list a: %s, %v", snap, err)
	}
	if env, _ := os.ReadFile(filepath.Join(snapDir, "a.env")); strings.TrimSpace(string(env)) != "T-1|Do it" {
		t.Errorf("hook env = %q", env)
	}

	tf, _ := taskFile("T-1")
	st, err := taskstate.Read(tf)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Repos) != 2 || st.Repos[0].Repo != "a" || st.Repos[1].Repo != "b" {
		t.Errorf("final state repos = %+v", st.Repos)
	}
	if _, err := os.Stat(tf + ".lock"); !os.IsNotExist(err) {
		t.Errorf("lock left behind: %v", err)
	}

	// The task exists now: a second new is a usage error.
	if err := cmdNew(newOpts{Ticket: "T-1", Repos: []string{"a"}, NoPatches: true}); err == nil || !cli.IsUsage(err) {
		t.Errorf("new on existing task: %v", err)
	}
}

func TestMvFinishesInterruptedRename(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)
	bases := t.TempDir()
	fleetEnv(t, `
defaults:
  default_branch: main
  base_root: `+bases+`
repos:
  - name: a
    origin: `+origin+`
`)
	if err := cmdNew(newOpts{Ticket: "OLD-1", Repos: []string{"a"}, NoPatches: true}); err != nil {
		t.Fatal(err)
	}
	if err := cmdMv("OLD-1", "NEW-1", false); err != nil {
		t.Fatalf("mv: %v", err)
	}

	// Recreate the crash window: new state written, old state not yet
	// removed (it already carries the moved paths).
	newFile, _ := taskFile("NEW-1")
	oldFile, _ := taskFile("OLD-1")
	st, err := taskstate.Read(newFile)
	if err != nil {
		t.Fatal(err)
	}
	st.Ticket = "OLD-1"
	if err := taskstate.Write(oldFile, st); err != nil {
		t.Fatal(err)
	}

	if err := cmdMv("OLD-1", "NEW-1", false); err != nil {
		t.Fatalf("rerun should finish the rename, got %v", err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old state file should be gone: %v", err)
	}

	// A genuinely different task at the target stays an error.
	st.Ticket = "OLD-1"
	if err := taskstate.Write(oldFile, st); err != nil {
		t.Fatal(err)
	}
	other, _ := taskstate.Read(newFile)
	other.Ticket = "SOMETHING-ELSE"
	if err := taskstate.Write(newFile, other); err != nil {
		t.Fatal(err)
	}
	if err := cmdMv("OLD-1", "NEW-1", false); err == nil || !cli.IsUsage(err) {
		t.Errorf("foreign target task: want usage error, got %v", err)
	}
}
