package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
)

func refExistsT(t *testing.T, base, ref string) bool {
	t.Helper()
	ok, err := linuxGit{}.RefExists(base, ref)
	if err != nil {
		t.Fatalf("RefExists(%s): %v", ref, err)
	}
	return ok
}

func TestEnsureBaseFreshThenIdempotent(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)
	base := filepath.Join(t.TempDir(), "bases", "r")

	for i := 0; i < 2; i++ {
		if err := ensureBase(linuxGit{}, "r", origin, base); err != nil {
			t.Fatalf("ensureBase run %d: %v", i, err)
		}
	}
	if got := gitT(t, base, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Errorf("base not bare: %s", got)
	}
	if got := gitT(t, base, "config", "remote.origin.url"); got != origin {
		t.Errorf("origin url = %s", got)
	}
	if !refExistsT(t, base, "refs/remotes/origin/main") {
		t.Error("origin/main not fetched")
	}
	if heads := gitT(t, base, "for-each-ref", "refs/heads"); heads != "" {
		t.Errorf("base should have no local branches, has %q", heads)
	}
}

func TestEnsureBaseCompletesInterruptedSetup(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)

	// Killed between `init --bare` and `remote add`.
	base := filepath.Join(t.TempDir(), "r")
	gitT(t, filepath.Dir(base), "init", "-q", "--bare", base)
	if err := ensureBase(linuxGit{}, "r", origin, base); err != nil {
		t.Fatalf("ensureBase on init-only base: %v", err)
	}
	if !refExistsT(t, base, "refs/remotes/origin/main") {
		t.Error("origin/main not fetched after completing setup")
	}

	// Killed right after mkdir: an empty directory.
	empty := filepath.Join(t.TempDir(), "r2")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureBase(linuxGit{}, "r", origin, empty); err != nil {
		t.Fatalf("ensureBase on empty dir: %v", err)
	}
	if !refExistsT(t, empty, "refs/remotes/origin/main") {
		t.Error("origin/main not fetched into formerly empty dir")
	}
}

func TestEnsureBaseRefusesForeignDirectory(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)
	base := filepath.Join(t.TempDir(), "r")
	writeFileT(t, filepath.Join(base, "notes.txt"), "mine")
	err := ensureBase(linuxGit{}, "r", origin, base)
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("want refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "notes.txt")); err != nil {
		t.Errorf("user file touched: %v", err)
	}
}

func TestEnsureBaseFetchPrunes(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)
	gitT(t, origin, "branch", "gone")
	base := filepath.Join(t.TempDir(), "r")
	if err := ensureBase(linuxGit{}, "r", origin, base); err != nil {
		t.Fatal(err)
	}
	if !refExistsT(t, base, "refs/remotes/origin/gone") {
		t.Fatal("setup: origin/gone not fetched")
	}
	gitT(t, origin, "branch", "-D", "gone")
	if err := ensureBase(linuxGit{}, "r", origin, base); err != nil {
		t.Fatal(err)
	}
	if refExistsT(t, base, "refs/remotes/origin/gone") {
		t.Error("deleted remote branch not pruned")
	}
}

func TestAddWorktreeModes(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)
	// A ticket branch someone already pushed, one commit ahead of main.
	gitT(t, origin, "checkout", "-q", "-b", "T-2")
	writeFileT(t, filepath.Join(origin, "pushed.txt"), "x")
	gitT(t, origin, "add", "pushed.txt")
	gitT(t, origin, "commit", "-q", "-m", "pushed work")
	gitT(t, origin, "checkout", "-q", "main")

	base := filepath.Join(t.TempDir(), "r")
	if err := ensureBase(linuxGit{}, "r", origin, base); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	g := linuxGit{}

	// (c) fresh branch from the default, no upstream.
	p1 := filepath.Join(wt, "T-1", "r")
	if err := addWorktree(g, base, p1, "T-1", "main"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := gitT(t, p1, "config", "--default", "", "branch.T-1.merge"); got != "" {
		t.Errorf("fresh branch must have no upstream, has %q", got)
	}

	// (b) the pushed branch is checked out tracking origin/T-2.
	p2 := filepath.Join(wt, "T-2", "r")
	if err := addWorktree(g, base, p2, "T-2", "main"); err != nil {
		t.Fatalf("track: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p2, "pushed.txt")); err != nil {
		t.Errorf("pushed work missing from worktree: %v", err)
	}
	if got := gitT(t, p2, "rev-parse", "--abbrev-ref", "T-2@{upstream}"); got != "origin/T-2" {
		t.Errorf("upstream = %q, want origin/T-2", got)
	}

	// (a) after removing the worktree, re-adding attaches the local branch
	// (keeping its local commit).
	writeFileT(t, filepath.Join(p1, "local.txt"), "y")
	gitT(t, p1, "add", "local.txt")
	gitT(t, p1, "commit", "-q", "-m", "local work")
	gitT(t, base, "worktree", "remove", "--force", p1)
	if err := addWorktree(g, base, p1, "T-1", "main"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p1, "local.txt")); err != nil {
		t.Errorf("attached worktree lost local commit: %v", err)
	}

	// Typo'd default branch: clear error naming it.
	err := addWorktree(g, base, filepath.Join(wt, "T-3", "r"), "T-3", "mian")
	if err == nil || !strings.Contains(err.Error(), "origin/mian") {
		t.Errorf("typo'd default: %v", err)
	}
}

func TestIsValidBranchName(t *testing.T) {
	requireGit(t)
	for name, want := range map[string]bool{
		"PROJ-1": true, "eng/PROJ-1": true, "a..b": false, "-x": false, "@{-1}": false, "a b": false, "x.lock": false,
	} {
		got, err := isValidBranchName(name)
		if err != nil || got != want {
			t.Errorf("isValidBranchName(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
}

func TestValidateBranchNames(t *testing.T) {
	requireGit(t)
	cfg := &config.ReposConfig{Repos: []config.RepoConfig{
		{Name: "a", BranchTemplate: "{description}"},
		{Name: "b", BranchTemplate: "x..{ticket}"},
		{Name: "c"},
	}}
	if err := validateBranchNames(cfg, []string{"c"}, "T-1", ""); err != nil {
		t.Errorf("plain template: %v", err)
	}
	for _, repo := range []string{"a", "b"} {
		err := validateBranchNames(cfg, []string{repo}, "T-1", "")
		if err == nil || !cli.IsUsage(err) || !strings.Contains(err.Error(), "repo "+repo) {
			t.Errorf("repo %s: want usage error naming it, got %v", repo, err)
		}
	}
}
