package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/taskstate"
)

// branchExists reports whether refs/heads/<branch> exists in base.
func branchExists(t *testing.T, base, branch string) bool {
	t.Helper()
	return gitT(t, base, "branch", "--list", branch) != ""
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func mustLoadConfig(t *testing.T) *config.ReposConfig {
	t.Helper()
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// oneRepoEnv sandboxes fleet-task with one repo "a" (origin with a main
// branch, base under a temp base_root) and returns the config dir and the
// base path.
func oneRepoEnv(t *testing.T) (cfgDir, base string) {
	t.Helper()
	requireGit(t)
	origin := makeOrigin(t)
	bases := t.TempDir()
	cfgDir = fleetEnv(t, `
defaults:
  default_branch: main
  base_root: `+bases+`
hooks:
  timeout: 30s
repos:
  - name: a
    origin: `+origin+`
`)
	return cfgDir, filepath.Join(bases, "a")
}

func readTask(t *testing.T, ticket string) *taskstate.State {
	t.Helper()
	tf, _ := taskFile(ticket)
	st, err := taskstate.Read(tf)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestBatchModeSSHEnv(t *testing.T) {
	env := func(set ...string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			for _, s := range set {
				if s == k {
					return "", true
				}
			}
			return "", false
		}
	}
	unset := func() (bool, error) { return false, nil }
	want := []string{"GIT_SSH_COMMAND=ssh -o BatchMode=yes"}
	if got := batchModeSSHEnv(false, env(), unset); !reflect.DeepEqual(got, want) {
		t.Errorf("headless, nothing set: %v", got)
	}
	if got := batchModeSSHEnv(true, env(), unset); got != nil {
		t.Errorf("interactive: %v", got)
	}
	for _, v := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
		if got := batchModeSSHEnv(false, env(v), unset); got != nil {
			t.Errorf("%s set: %v", v, got)
		}
	}
	if got := batchModeSSHEnv(false, env(), func() (bool, error) { return true, nil }); got != nil {
		t.Errorf("core.sshCommand set: %v", got)
	}
	if got := batchModeSSHEnv(false, env(), func() (bool, error) { return false, os.ErrPermission }); got != nil {
		t.Errorf("lookup failed: %v", got)
	}
}

func TestBranchModeFromFlags(t *testing.T) {
	if _, err := branchModeFromFlags(true, true); !cli.IsUsage(err) {
		t.Errorf("both: %v", err)
	}
	for _, c := range []struct {
		keep, del bool
		want      string
	}{{false, false, branchAuto}, {true, false, branchKeep}, {false, true, branchDelete}} {
		if got, err := branchModeFromFlags(c.keep, c.del); err != nil || got != c.want {
			t.Errorf("%v/%v: %q, %v", c.keep, c.del, got, err)
		}
	}
}

func TestCleanupBranchOutcomes(t *testing.T) {
	requireGit(t)
	origin := makeOrigin(t)
	base := filepath.Join(t.TempDir(), "base")
	g := linuxGit{}
	if err := ensureBase(g, "r", origin, base); err != nil {
		t.Fatal(err)
	}
	if o := cleanupBranch(g, base, false, "x", branchAuto); o.Deleted || !o.Warn || !strings.Contains(o.KeptReason, "base is unknown") {
		t.Errorf("unknown base: %+v", o)
	}
	if o := cleanupBranch(g, base, true, "-x", branchDelete); o.Deleted || !o.Warn {
		t.Errorf("option-like name: %+v", o)
	}
	if o := cleanupBranch(g, base, true, "missing", branchAuto); o.Deleted || o.Warn || !strings.Contains(o.KeptReason, "no local branch") {
		t.Errorf("missing branch: %+v", o)
	}
	// Checked out in a worktree: git refuses; reported, kept.
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, base, "worktree", "add", "-q", "-b", "busy", wt, "origin/main")
	if o := cleanupBranch(g, base, true, "busy", branchDelete); o.Deleted || !o.Warn || !strings.Contains(o.KeptReason, "git did not delete it") {
		t.Errorf("checked-out branch: %+v", o)
	}
}

func TestCoreSSHCommandSet(t *testing.T) {
	requireGit(t)
	base := filepath.Join(t.TempDir(), "base")
	gitT(t, filepath.Dir(base), "init", "-q", "--bare", base)
	if set, err := coreSSHCommandSet(base); err != nil || set {
		t.Errorf("unset: %v, %v", set, err)
	}
	gitT(t, base, "config", "core.sshCommand", "ssh -i key")
	if set, err := coreSSHCommandSet(base); err != nil || !set {
		t.Errorf("set: %v, %v", set, err)
	}
}

func TestDecideAdoption(t *testing.T) {
	cases := []struct {
		at        worktreeAtPath
		wantAdopt bool
		wantErr   string
	}{
		{at: worktreeAtPath{}},
		{at: worktreeAtPath{Exists: true, Registered: true, Ref: "refs/heads/T-1"}, wantAdopt: true},
		{at: worktreeAtPath{Exists: true, Registered: true}, wantErr: "detached HEAD"},
		{at: worktreeAtPath{Exists: true, Registered: true, Ref: "refs/heads/x"}, wantErr: "on x instead of branch T-1"},
		{at: worktreeAtPath{Exists: true, Found: "a file, not a worktree of /b"}, wantErr: "is a file, not a worktree of /b"},
	}
	for _, c := range cases {
		adopt, err := decideAdoption(c.at, "/wt/T-1/a", "/b", "T-1")
		if adopt != c.wantAdopt || (c.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%+v: adopt=%v err=%v", c.at, adopt, err)
		}
	}
}

// TestFetchNetworkTimeout: a remote that never answers (git's ext::
// transport running sleep) is killed after git.network_timeout.
func TestFetchNetworkTimeout(t *testing.T) {
	requireGit(t)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.ext.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	base := filepath.Join(t.TempDir(), "base")
	gitT(t, filepath.Dir(base), "init", "-q", "--bare", base)
	gitT(t, base, "remote", "add", "origin", "ext::sleep 60")

	cfg, err := config.Parse([]byte("git:\n  network_timeout: 500ms\n"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = linuxGit{cfg: cfg}.Fetch(base, "ext::sleep 60")
	if err == nil || !strings.Contains(err.Error(), "killed after the 500ms timeout") || !strings.Contains(err.Error(), "git.network_timeout") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("fetch took %s", time.Since(start))
	}
}

func TestNewAdoptsLeftoverWorktree(t *testing.T) {
	_, base := oneRepoEnv(t)
	if err := cmdNew(newOpts{Ticket: "T-1", Repos: []string{"a"}, NoPatches: true}); err != nil {
		t.Fatal(err)
	}
	// Simulate `new` killed after `worktree add` but before the state write.
	tf, _ := taskFile("T-1")
	wt := readTask(t, "T-1").Repos[0].WorktreePath
	if err := os.Remove(tf); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(wt, "unsaved.txt")
	writeFileT(t, marker, "keep me\n")

	results, failed, err := addRepos(mustLoadConfig(t), &taskstate.State{Ticket: "T-1"}, tf, filepath.Dir(filepath.Dir(wt)), []string{"a"}, map[string][]string{"a": nil})
	if err != nil || failed != 0 {
		t.Fatalf("adopting: failed=%d err=%v results=%+v", failed, err, results)
	}
	if !results[0].Adopted || results[0].Patches != nil {
		t.Errorf("result = %+v, want adopted with unknown (nil) patches", results[0])
	}
	if !fileExists(marker) {
		t.Error("adoption touched the worktree's files")
	}
	if st := readTask(t, "T-1"); len(st.Repos) != 1 || st.Repos[0].Patches != nil {
		t.Errorf("state = %+v", st.Repos)
	}

	// The same path on another branch is an error naming the branch.
	gitT(t, wt, "switch", "-q", "-c", "other")
	_, _, _, err = createRepoWorktree(mustLoadConfig(t), mustLoadConfig(t).FindRepo("a"), "T-1", "", filepath.Dir(filepath.Dir(wt)), nil)
	if err == nil || !strings.Contains(err.Error(), "on other instead of branch T-1") {
		t.Errorf("other branch: %v", err)
	}
	gitT(t, wt, "switch", "-q", "T-1")

	// A plain directory at another repo path is an error, left alone.
	plain := filepath.Join(filepath.Dir(filepath.Dir(wt)), "T-2", "a")
	writeFileT(t, filepath.Join(plain, "notes"), "mine\n")
	_, _, _, err = createRepoWorktree(mustLoadConfig(t), mustLoadConfig(t).FindRepo("a"), "T-2", "", filepath.Dir(filepath.Dir(wt)), nil)
	if err == nil || !strings.Contains(err.Error(), "a directory that is not a git worktree of "+base) {
		t.Errorf("plain dir: %v", err)
	}
	if !fileExists(filepath.Join(plain, "notes")) {
		t.Error("plain dir was touched")
	}
}

func TestParseWorktreeList(t *testing.T) {
	out := "worktree /b\r\nbare\r\n\r\nworktree C:/wt/T-1/a\r\nHEAD 123\r\nbranch refs/heads/T-1\r\n\r\nworktree /wt/x\nHEAD 456\ndetached\n"
	want := []worktreeEntry{{Path: "/b", Bare: true}, {Path: "C:/wt/T-1/a", Ref: "refs/heads/T-1"}, {Path: "/wt/x"}}
	if got := parseWorktreeList(out); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	if windowsPathKey(`C:/WT/T-1/a/`) != windowsPathKey(`c:\wt\T-1\A`) {
		t.Error("windowsPathKey should ignore case, slash style and a trailing separator")
	}
}

func TestPreCreateHookVetoes(t *testing.T) {
	cfgDir, _ := oneRepoEnv(t)
	writeHook(t, cfgDir, "pre-create", "10-no", "#!/bin/sh\nexit 7\n")
	marker := filepath.Join(t.TempDir(), "post")
	writeHook(t, cfgDir, "post-create", "10-mark", "#!/bin/sh\ntouch "+marker+"\n")

	err := cmdNew(newOpts{Ticket: "T-1", Repos: []string{"a"}, NoPatches: true})
	if err == nil || cli.IsUsage(err) {
		t.Fatalf("want a failure, got %v", err)
	}
	wt := filepath.Join(os.Getenv("FLEET_STATE_DIR"), "worktrees", "T-1", "a")
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("vetoed repo got a worktree: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("post-create hook ran for a vetoed repo")
	}
	if tf, _ := taskFile("T-1"); fileExists(tf) {
		t.Error("state written though no repo was set up")
	}
}

func TestPreRemoveHookVetoesAndKeepsRepo(t *testing.T) {
	cfgDir, _ := oneRepoEnv(t)
	if err := cmdNew(newOpts{Ticket: "T-1", Repos: []string{"a"}, NoPatches: true}); err != nil {
		t.Fatal(err)
	}
	writeHook(t, cfgDir, "pre-remove", "10-no", "#!/bin/sh\nexit 5\n")
	err := cmdRm("T-1", false, branchAuto, false)
	if err == nil || !strings.Contains(err.Error(), "1 repo(s) failed") {
		t.Fatalf("want a partial failure, got %v", err)
	}
	st := readTask(t, "T-1")
	if len(st.Repos) != 1 || !fileExists(st.Repos[0].WorktreePath) {
		t.Errorf("vetoed repo should stay, with its worktree: %+v", st.Repos)
	}
}

func TestRmBranchCleanup(t *testing.T) {
	_, base := oneRepoEnv(t)
	newTask := func(ticket string, commit bool) string {
		t.Helper()
		if err := cmdNew(newOpts{Ticket: ticket, Repos: []string{"a"}, NoPatches: true}); err != nil {
			t.Fatal(err)
		}
		wt := readTask(t, ticket).Repos[0].WorktreePath
		if commit {
			writeFileT(t, filepath.Join(wt, "work.txt"), "work\n")
			gitT(t, wt, "add", "work.txt")
			gitT(t, wt, "commit", "-q", "-m", "local work")
		}
		return wt
	}

	// Nothing committed: every commit is on origin/main, so it goes.
	newTask("T-1", false)
	if err := cmdRm("T-1", false, branchAuto, false); err != nil {
		t.Fatal(err)
	}
	if branchExists(t, base, "T-1") {
		t.Error("fully pushed branch T-1 was kept")
	}

	// An unpushed commit: kept by default.
	newTask("T-2", true)
	if err := cmdRm("T-2", false, branchAuto, false); err != nil {
		t.Fatal(err)
	}
	if !branchExists(t, base, "T-2") {
		t.Error("branch T-2 with unpushed work was deleted")
	}

	// --keep-branch keeps even a fully pushed branch.
	newTask("T-3", false)
	if err := cmdRm("T-3", false, branchKeep, false); err != nil {
		t.Fatal(err)
	}
	if !branchExists(t, base, "T-3") {
		t.Error("--keep-branch: branch deleted")
	}

	// --delete-branch with unpushed work and no terminal: usage error,
	// nothing removed; with --force it goes.
	wt := newTask("T-4", true)
	if err := cmdRm("T-4", false, branchDelete, false); !cli.IsUsage(err) || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want usage error naming --force, got %v", err)
	}
	if !fileExists(wt) || !branchExists(t, base, "T-4") {
		t.Error("refused --delete-branch still removed something")
	}
	if err := cmdRm("T-4", true, branchDelete, false); err != nil {
		t.Fatal(err)
	}
	if branchExists(t, base, "T-4") {
		t.Error("--delete-branch --force: branch kept")
	}
}

// writeHook writes an executable hook script for phase.
func writeHook(t *testing.T, cfgDir, phase, name, body string) {
	t.Helper()
	p := filepath.Join(cfgDir, "hooks", phase, name)
	writeFileT(t, p, body)
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
