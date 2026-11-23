package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

func TestHookEnv(t *testing.T) {
	got := hookEnv([]string{"PATH=/bin"}, hookContext{Ticket: "T-1", Description: "Fix it", Repo: "api", Branch: "eng/T-1", WorktreeDir: "/wt/T-1/api"})
	want := []string{"PATH=/bin", "FLEET_TICKET=T-1", "FLEET_DESCRIPTION=Fix it", "FLEET_REPO=api", "FLEET_BRANCH=eng/T-1", "FLEET_WORKTREE_DIR=/wt/T-1/api"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hookEnv = %v\nwant %v", got, want)
	}
}

func TestTmuxArgs(t *testing.T) {
	t.Setenv("FLEET_TMUX_SOCKET", "")
	if got := tmuxArgs("show-options", "-g"); !reflect.DeepEqual(got, []string{"show-options", "-g"}) {
		t.Errorf("no socket: %v", got)
	}
	t.Setenv("FLEET_TMUX_SOCKET", "fleet-test")
	if got := tmuxArgs("show-options", "-g"); !reflect.DeepEqual(got, []string{"-L", "fleet-test", "show-options", "-g"}) {
		t.Errorf("socket: %v", got)
	}
}

func TestResolveVersion(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123"},
		{Key: "vcs.modified", Value: "true"},
	}}
	cases := []struct {
		v    string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"1.2.3", info, true, "1.2.3"},
		{"dev", info, true, "0123456789ab-dirty"},
		{"dev", &debug.BuildInfo{}, true, "dev"},
		{"dev", nil, false, "dev"},
	}
	for _, c := range cases {
		if got := resolveVersion(c.v, c.info, c.ok); got != c.want {
			t.Errorf("resolveVersion(%q) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestGitEnvDisablesPromptsHeadless(t *testing.T) {
	t.Setenv("FLEET_NO_INPUT", "1")
	if !slices.Contains(gitEnv(), "GIT_TERMINAL_PROMPT=0") {
		t.Error("headless git env must set GIT_TERMINAL_PROMPT=0")
	}
	env := gitEnv("GIT_INDEX_FILE=/x")
	if env[len(env)-1] != "GIT_INDEX_FILE=/x" {
		t.Error("extra entries must come last so they win")
	}
}

func TestReplaceSymlink(t *testing.T) {
	dir := t.TempDir()
	target1 := filepath.Join(dir, "t1")
	target2 := filepath.Join(dir, "t2")
	link := filepath.Join(dir, "sub", "link")

	if err := replaceSymlink(link, target1); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := replaceSymlink(link, target2); err != nil {
		t.Fatalf("replace symlink: %v", err)
	}
	if got, _ := os.Readlink(link); got != target2 {
		t.Errorf("link -> %q, want %q", got, target2)
	}

	real := filepath.Join(dir, "realdir")
	writeFileT(t, filepath.Join(real, "work.txt"), "precious")
	if err := replaceSymlink(real, target1); err == nil {
		t.Error("a real directory must not be replaced")
	}
	if _, err := os.Stat(filepath.Join(real, "work.txt")); err != nil {
		t.Errorf("real directory contents touched: %v", err)
	}
}

func TestWindowsDirIsEmptyScript(t *testing.T) {
	s := windowsDirIsEmptyScript(`C:\it's`)
	for _, want := range []string{"$ErrorActionPreference='Stop'", "-Force", "-LiteralPath 'C:\\it''s'", "exit 3", "catch", "exit 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
}

func TestListTasksPrefersFilename(t *testing.T) {
	dir := t.TempDir()
	if err := writeTaskState(filepath.Join(dir, "REAL-1.json"), &TaskState{Ticket: "WRONG-1"}); err != nil {
		t.Fatal(err)
	}
	got, err := listTasks(dir)
	if err != nil || len(got) != 1 || got[0].Ticket != "REAL-1" {
		t.Fatalf("listTasks = %+v, %v", got, err)
	}
}

func TestRunHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"version"}, {"--version"}} {
		stdout := captureStdout(t, func() {
			if err := run(args); err != nil && !strings.Contains(err.Error(), "help requested") {
				t.Errorf("run(%v) = %v", args, err)
			}
		})
		if !strings.Contains(stdout, "fleet-task") {
			t.Errorf("run(%v) printed %q on stdout", args, stdout)
		}
	}
}

// captureStdout runs f with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}

func TestUnknownTicketIsUsageError(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	t.Setenv("FLEET_REPOS_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("FLEET_NO_INPUT", "1")
	t.Setenv("FLEET_TMUX_SOCKET", "fleet-task-test-none")
	for _, args := range [][]string{
		{"show", "NOPE-1"}, {"rm", "NOPE-1"}, {"edit", "NOPE-1", "--add", "x"}, {"jump", "NOPE-1"}, {"mv", "NOPE-1", "NEW-1"},
	} {
		err := run(args)
		if err == nil || !isUsageError(err) || !strings.Contains(err.Error(), "no task found for ticket NOPE-1") {
			t.Errorf("run(%v) = %v; want usage error naming the ticket", args, err)
		}
	}
}

func TestMvUsageErrors(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	t.Setenv("FLEET_REPOS_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("FLEET_TMUX_SOCKET", "fleet-task-test-none")
	for _, ticket := range []string{"A-1", "B-1"} {
		tf, _ := taskFile(ticket)
		if err := writeTaskState(tf, &TaskState{Ticket: ticket, Repos: []TaskRepo{{Repo: "r", WorktreePath: "/nonexistent/" + ticket + "/r"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmdMv("A-1", "A-1", false); err == nil || !isUsageError(err) {
		t.Errorf("same name: %v", err)
	}
	if err := cmdMv("A-1", "B-1", false); err == nil || !isUsageError(err) || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("existing target: %v", err)
	}
}
