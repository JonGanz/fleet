package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtureYAML = `
tmux:
  session_name: fleet
  config_file: ~/.config/fleet/tmux.conf

worktree_root: ~/.local/state/fleet/worktrees

repos:
  - name: backend
    origin: git@github.com:org/backend.git
    base: ~/dev/.fleet-base/backend
    default_branch: main
    runtime: linux
    run:
      - name: api
        cmd: "npm run start:dev"
      - name: worker
        cmd: "npm run start:worker"

  - name: admin-ui
    origin: git@github.com:org/admin-ui.git
    base: ~/dev/.fleet-base/admin-ui
    default_branch: main
    runtime: windows
    run:
      - name: dev
        cmd: "npm run dev"
`

const defaultsFixtureYAML = `
defaults:
  default_branch: main
  branch_template: "eng/{ticket}"
  base_root: ~/dev/.fleet-base
  windows_base_root: ~/dev/.fleet-base-windows

worktree_root: ~/.local/state/fleet/worktrees
windows_worktree_root: ~/dev/.fleet-worktrees-windows
windows_cache_root: ~/dev/.fleet-cache-windows

repos:
  - name: backend
    origin: git@github.com:org/backend.git
    run:
      - name: api
        cmd: "npm run start:dev"

  - name: admin-ui
    origin: git@github.com:org/admin-ui.git
    base: ~/dev/.fleet-base/admin-ui-custom
    default_branch: develop
    branch_template: "ui/{ticket}"
    run:
      - name: dev
        cmd: "npm run dev"

  - name: room-launcher
    origin: git@github.com:org/room-launcher.git
    runtime: windows
    run:
      - name: dev
        cmd: "npm run dev"

  - name: staff-quiosk
    origin: git@github.com:org/staff-quiosk.git
    runtime: windows
    windows_base: ~/dev/.fleet-base-windows/staff-quiosk-custom
    run:
      - name: dev
        cmd: "npm run dev"
`

func TestLoadReposConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "repos.yaml", fixtureYAML)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("loadReposConfig: %v", err)
	}

	if cfg.Tmux.SessionName != "fleet" {
		t.Errorf("Tmux.SessionName = %q, want %q", cfg.Tmux.SessionName, "fleet")
	}
	if cfg.Tmux.ConfigFile != "~/.config/fleet/tmux.conf" {
		t.Errorf("Tmux.ConfigFile = %q, want %q", cfg.Tmux.ConfigFile, "~/.config/fleet/tmux.conf")
	}
	if cfg.WorktreeRoot != "~/.local/state/fleet/worktrees" {
		t.Errorf("WorktreeRoot = %q, want %q", cfg.WorktreeRoot, "~/.local/state/fleet/worktrees")
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("len(Repos) = %d, want 2", len(cfg.Repos))
	}

	backend := cfg.Repos[0]
	if backend.Name != "backend" {
		t.Errorf("Repos[0].Name = %q, want backend", backend.Name)
	}
	if backend.Origin != "git@github.com:org/backend.git" {
		t.Errorf("Repos[0].Origin = %q", backend.Origin)
	}
	if backend.Base != "~/dev/.fleet-base/backend" {
		t.Errorf("Repos[0].Base = %q", backend.Base)
	}
	if backend.DefaultBranch != "main" {
		t.Errorf("Repos[0].DefaultBranch = %q", backend.DefaultBranch)
	}
	if backend.BranchTemplate != "" {
		t.Errorf("Repos[0].BranchTemplate = %q, want empty (no defaults.branch_template set)", backend.BranchTemplate)
	}
	if backend.Runtime != "linux" {
		t.Errorf("Repos[0].Runtime = %q", backend.Runtime)
	}
	if len(backend.Run) != 2 || backend.Run[0].Name != "api" || backend.Run[0].Cmd != "npm run start:dev" {
		t.Errorf("Repos[0].Run = %+v", backend.Run)
	}

	adminUI := cfg.Repos[1]
	if adminUI.Name != "admin-ui" || adminUI.Runtime != "windows" {
		t.Errorf("Repos[1] = %+v", adminUI)
	}

	if got := cfg.FindRepo("backend"); got == nil || got.Name != "backend" {
		t.Errorf("FindRepo(backend) = %+v", got)
	}
	if got := cfg.FindRepo("nonexistent"); got != nil {
		t.Errorf("FindRepo(nonexistent) = %+v, want nil", got)
	}

	names := cfg.RepoNames()
	if len(names) != 2 || names[0] != "backend" || names[1] != "admin-ui" {
		t.Errorf("RepoNames() = %v", names)
	}
}

func TestLoadReposConfigAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "repos.yaml", defaultsFixtureYAML)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("loadReposConfig: %v", err)
	}

	backend := cfg.FindRepo("backend")
	if backend == nil {
		t.Fatal("FindRepo(backend) = nil")
	}
	if backend.Base != "~/dev/.fleet-base/backend" {
		t.Errorf("backend.Base = %q, want default-derived path", backend.Base)
	}
	if backend.DefaultBranch != "main" {
		t.Errorf("backend.DefaultBranch = %q, want %q", backend.DefaultBranch, "main")
	}
	if backend.BranchTemplate != "eng/{ticket}" {
		t.Errorf("backend.BranchTemplate = %q, want default-derived %q", backend.BranchTemplate, "eng/{ticket}")
	}

	adminUI := cfg.FindRepo("admin-ui")
	if adminUI == nil {
		t.Fatal("FindRepo(admin-ui) = nil")
	}
	if adminUI.Base != "~/dev/.fleet-base/admin-ui-custom" {
		t.Errorf("adminUI.Base = %q, want explicit value preserved", adminUI.Base)
	}
	if adminUI.DefaultBranch != "develop" {
		t.Errorf("adminUI.DefaultBranch = %q, want explicit value preserved", adminUI.DefaultBranch)
	}
	if adminUI.BranchTemplate != "ui/{ticket}" {
		t.Errorf("adminUI.BranchTemplate = %q, want explicit value preserved", adminUI.BranchTemplate)
	}

	if cfg.WindowsWorktreeRoot != "~/dev/.fleet-worktrees-windows" {
		t.Errorf("WindowsWorktreeRoot = %q", cfg.WindowsWorktreeRoot)
	}
	if cfg.WindowsCacheRoot != "~/dev/.fleet-cache-windows" {
		t.Errorf("WindowsCacheRoot = %q", cfg.WindowsCacheRoot)
	}

	roomLauncher := cfg.FindRepo("room-launcher")
	if roomLauncher == nil {
		t.Fatal("FindRepo(room-launcher) = nil")
	}
	if roomLauncher.WindowsBase != "~/dev/.fleet-base-windows/room-launcher" {
		t.Errorf("roomLauncher.WindowsBase = %q, want default-derived path", roomLauncher.WindowsBase)
	}
	if roomLauncher.DefaultBranch != "main" {
		t.Errorf("roomLauncher.DefaultBranch = %q, want %q", roomLauncher.DefaultBranch, "main")
	}
	if roomLauncher.BranchTemplate != "eng/{ticket}" {
		t.Errorf("roomLauncher.BranchTemplate = %q, want default-derived %q", roomLauncher.BranchTemplate, "eng/{ticket}")
	}
	// A runtime: windows repo should NOT get a plain (Linux) `base` derived
	// from defaults.base_root -- only windows_base applies.
	if roomLauncher.Base != "" {
		t.Errorf("roomLauncher.Base = %q, want empty (windows-runtime repos don't use base_root)", roomLauncher.Base)
	}

	staffQuiosk := cfg.FindRepo("staff-quiosk")
	if staffQuiosk == nil {
		t.Fatal("FindRepo(staff-quiosk) = nil")
	}
	if staffQuiosk.WindowsBase != "~/dev/.fleet-base-windows/staff-quiosk-custom" {
		t.Errorf("staffQuiosk.WindowsBase = %q, want explicit value preserved", staffQuiosk.WindowsBase)
	}
}

func TestLoadReposConfigRejectsInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "repos.yaml")
	if err := os.WriteFile(p, []byte("repos:\n  - name: a\n    runtime: macos\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "macos") {
		t.Errorf("want runtime error, got %v", err)
	}
}

func TestParseAppliesDefaultsAndValidates(t *testing.T) {
	cfg, err := Parse([]byte(defaultsFixtureYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r := cfg.FindRepo("backend"); r == nil || r.Base != "~/dev/.fleet-base/backend" || r.IsWindows() {
		t.Errorf("backend = %+v", r)
	}
	if r := cfg.FindRepo("room-launcher"); r == nil || !r.IsWindows() {
		t.Errorf("room-launcher = %+v", r)
	}
	if _, err := Parse([]byte("repos:\n  - name: a\n  - name: a\n")); err == nil {
		t.Error("Parse must validate (duplicate repo names)")
	}
	if _, err := Parse([]byte("repos: [")); err == nil {
		t.Error("Parse must reject malformed YAML")
	}
	var nilCfg *ReposConfig
	if nilCfg.FindRepo("x") != nil {
		t.Error("FindRepo on a nil config must return nil")
	}
}

func TestReposConfigValidate(t *testing.T) {
	cases := []struct {
		name  string
		repos []RepoConfig
		ok    bool
	}{
		{"fine", []RepoConfig{{Name: "a"}, {Name: "b", Runtime: "linux"}, {Name: "c", Runtime: "windows"}}, true},
		{"empty name", []RepoConfig{{Name: ""}}, false},
		{"dup", []RepoConfig{{Name: "a"}, {Name: "a"}}, false},
		{"slash", []RepoConfig{{Name: "a/b"}}, false},
		{"backslash", []RepoConfig{{Name: `a\b`}}, false},
		{"dot", []RepoConfig{{Name: "."}}, false},
		{"dotdot", []RepoConfig{{Name: ".."}}, false},
		{"bad runtime", []RepoConfig{{Name: "a", Runtime: "Windows"}}, false},
	}
	for _, c := range cases {
		err := (&ReposConfig{Repos: c.repos}).validate()
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestTimeouts(t *testing.T) {
	cfg, err := Parse([]byte("repos: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HookTimeout() != DefaultHookTimeout || cfg.NetworkTimeout() != DefaultNetworkTimeout {
		t.Errorf("defaults: hook %s, network %s", cfg.HookTimeout(), cfg.NetworkTimeout())
	}
	var nilCfg *ReposConfig
	if nilCfg.HookTimeout() != DefaultHookTimeout || nilCfg.NetworkTimeout() != DefaultNetworkTimeout {
		t.Error("nil config must give the defaults")
	}

	cfg, err = Parse([]byte("hooks:\n  timeout: 90s\ngit:\n  network_timeout: 1h30m\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HookTimeout() != 90*time.Second || cfg.NetworkTimeout() != 90*time.Minute {
		t.Errorf("configured: hook %s, network %s", cfg.HookTimeout(), cfg.NetworkTimeout())
	}

	bad := map[string]string{
		"hooks:\n  timeout: 0\n":                "must be more than 0",
		"hooks:\n  timeout: 0s\n":               "must be more than 0",
		"hooks:\n  timeout: -5s\n":              "must be more than 0",
		"hooks:\n  timeout: 90\n":               "is not a duration",
		"hooks:\n  timeout: soon\n":             "is not a duration",
		"git:\n  network_timeout: 25h\n":        "at most 24h0m0s",
		"git:\n  network_timeout: \"0\"\n":      "git.network_timeout",
		"git:\n  network_timeout: 10 minutes\n": "is not a duration",
	}
	for body, want := range bad {
		if _, err := Parse([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want it to mention %q", body, err, want)
		}
	}
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", p, err)
	}
	return p
}
