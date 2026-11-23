package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/hooks"
	"github.com/JonGanz/fleet/internal/taskstate"
)

func TestHookEnv(t *testing.T) {
	ts := &taskstate.State{Ticket: "T-1", Description: "desc"}
	p := RunPair{Repo: "backend", RunName: "api", WorktreeDir: "/wt", Branch: "eng/T-1"}
	want := []string{
		"FLEET_TICKET=T-1", "FLEET_DESCRIPTION=desc", "FLEET_REPO=backend",
		"FLEET_BRANCH=eng/T-1", "FLEET_WORKTREE_DIR=/wt", "FLEET_RUN_NAME=api",
	}
	if got := hookEnv(ts, p).Vars(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestRunRunHooks runs real hook scripts: sorted order, env, and a failing
// hook not stopping the next one.
func TestRunRunHooks(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("FLEET_CONFIG_DIR", cfgDir)
	dir := filepath.Join(cfgDir, "hooks", hooks.PreRun)
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
	runRunHooks(hooks.PreRun, &taskstate.State{Ticket: "T-1"}, RunPair{Repo: "web", RunName: "dev", Branch: "b"})
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("hook after a failing one did not run: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "T-1 web dev b" {
		t.Errorf("hook saw %q", got)
	}
	// No post-run dir: nothing happens, no panic.
	runRunHooks(hooks.PostRun, &taskstate.State{Ticket: "T-1"}, RunPair{})
}
