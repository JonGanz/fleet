package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverHooks(t *testing.T) {
	dir := t.TempDir()
	for name, mode := range map[string]os.FileMode{"20-b": 0o755, "10-a": 0o755, "30-noexec": 0o644} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "40-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := discoverHooks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "10-a"), filepath.Join(dir, "20-b")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, err := discoverHooks(filepath.Join(dir, "missing")); err != nil || got != nil {
		t.Errorf("missing dir: %v, %v", got, err)
	}
}

func TestHookEnv(t *testing.T) {
	ts := &TaskState{Ticket: "T-1", Description: "desc"}
	p := RunPair{Repo: "backend", RunName: "api", WorktreeDir: "/wt", Branch: "eng/T-1"}
	want := []string{
		"FLEET_TICKET=T-1", "FLEET_REPO=backend", "FLEET_WORKTREE_DIR=/wt",
		"FLEET_BRANCH=eng/T-1", "FLEET_DESCRIPTION=desc", "FLEET_RUN_NAME=api",
	}
	if got := hookEnv(ts, p); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestRunRunHooks runs real hook scripts: sorted order, env, and a failing
// hook not stopping the next one.
func TestRunRunHooks(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("FLEET_CONFIG_DIR", cfgDir)
	dir := filepath.Join(cfgDir, "hooks", hookPhasePreRun)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	scripts := map[string]string{
		"10-fail": "#!/bin/sh\nexit 3\n",
		"20-log":  "#!/bin/sh\necho \"$FLEET_TICKET $FLEET_REPO $FLEET_RUN_NAME $FLEET_BRANCH\" >> " + out + "\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runRunHooks(hookPhasePreRun, &TaskState{Ticket: "T-1"}, RunPair{Repo: "web", RunName: "dev", Branch: "b"})
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("hook after a failing one did not run: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "T-1 web dev b" {
		t.Errorf("hook saw %q", got)
	}
	// No post-run dir: nothing happens, no panic.
	runRunHooks(hookPhasePostRun, &TaskState{Ticket: "T-1"}, RunPair{})
}
