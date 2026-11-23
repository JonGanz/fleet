package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

// TestRunRunHooks runs real hook scripts: env, a failing pre-run hook
// vetoing (and stopping the hooks after it), a failing post-run hook only
// warning.
func TestRunRunHooks(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("FLEET_CONFIG_DIR", cfgDir)
	out := filepath.Join(t.TempDir(), "out")
	write := func(phase, name, body string) {
		dir := filepath.Join(cfgDir, "hooks", phase)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logHook := "#!/bin/sh\necho \"$FLEET_TICKET $FLEET_REPO $FLEET_RUN_NAME $FLEET_BRANCH\" >> " + out + "\n"
	write(hooks.PreRun, "10-log", logHook)
	write(hooks.PreRun, "20-fail", "#!/bin/sh\nexit 3\n")
	write(hooks.PreRun, "30-log", logHook)
	write(hooks.PostRun, "10-fail", "#!/bin/sh\nexit 3\n")
	write(hooks.PostRun, "20-log", logHook)

	ts := &taskstate.State{Ticket: "T-1"}
	pair := RunPair{Repo: "web", RunName: "dev", Branch: "b"}
	err := runRunHooks(hooks.PreRun, ts, pair, time.Minute)
	if !hooks.IsVeto(err) || !strings.Contains(err.Error(), "20-fail failed: exit status 3") {
		t.Fatalf("pre-run: want a veto by 20-fail, got %v", err)
	}
	if err := runRunHooks(hooks.PostRun, ts, pair, time.Minute); err != nil {
		t.Fatalf("post-run failures must only warn, got %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// 10-log (pre), not 30-log (after the veto), then 20-log (post).
	if got := strings.TrimSpace(string(data)); got != "T-1 web dev b\nT-1 web dev b" {
		t.Errorf("hooks logged %q", got)
	}
}
