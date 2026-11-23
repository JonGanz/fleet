package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/taskstate"
)

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

func TestMvUsageErrors(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	t.Setenv("FLEET_REPOS_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("FLEET_TMUX_SOCKET", "fleet-task-test-none")
	for _, ticket := range []string{"A-1", "B-1"} {
		tf, _ := taskFile(ticket)
		if err := taskstate.Write(tf, &taskstate.State{Ticket: ticket, Repos: []taskstate.Repo{{Repo: "r", WorktreePath: "/nonexistent/" + ticket + "/r"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmdMv("A-1", "A-1", false); err == nil || !cli.IsUsage(err) {
		t.Errorf("same name: %v", err)
	}
	if err := cmdMv("A-1", "B-1", false); err == nil || !cli.IsUsage(err) || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("existing target: %v", err)
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

func TestUnknownTicketIsUsageError(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	t.Setenv("FLEET_REPOS_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("FLEET_NO_INPUT", "1")
	t.Setenv("FLEET_TMUX_SOCKET", "fleet-task-test-none")
	for _, args := range [][]string{
		{"show", "NOPE-1"}, {"rm", "NOPE-1"}, {"edit", "NOPE-1", "--add", "x"}, {"jump", "NOPE-1"}, {"mv", "NOPE-1", "NEW-1"},
	} {
		err := run(args)
		if err == nil || !cli.IsUsage(err) || !strings.Contains(err.Error(), "no task found for ticket NOPE-1") {
			t.Errorf("run(%v) = %v; want usage error naming the ticket", args, err)
		}
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
