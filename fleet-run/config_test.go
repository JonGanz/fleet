package main

import "testing"

// reposYAMLFixture mirrors the example in docs/CONTRACT.md.
const reposYAMLFixture = `
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

func TestParseReposConfig(t *testing.T) {
	cfg, err := parseReposConfig([]byte(reposYAMLFixture))
	if err != nil {
		t.Fatalf("parseReposConfig: %v", err)
	}

	if got, want := cfg.Tmux.SessionName, "fleet"; got != want {
		t.Errorf("tmux.session_name = %q, want %q", got, want)
	}
	if got, want := cfg.Tmux.ConfigFile, "~/.config/fleet/tmux.conf"; got != want {
		t.Errorf("tmux.config_file = %q, want %q", got, want)
	}
	if got, want := cfg.WorktreeRoot, "~/.local/state/fleet/worktrees"; got != want {
		t.Errorf("worktree_root = %q, want %q", got, want)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("len(Repos) = %d, want 2", len(cfg.Repos))
	}

	backend := cfg.findRepo("backend")
	if backend == nil {
		t.Fatal("findRepo(backend) = nil")
	}
	if backend.Runtime != "linux" {
		t.Errorf("backend.Runtime = %q, want linux", backend.Runtime)
	}
	if backend.IsWindows() {
		t.Error("backend.IsWindows() = true, want false")
	}
	if len(backend.Run) != 2 {
		t.Fatalf("len(backend.Run) = %d, want 2", len(backend.Run))
	}
	if backend.Run[0].Name != "api" || backend.Run[0].Cmd != "npm run start:dev" {
		t.Errorf("backend.Run[0] = %+v", backend.Run[0])
	}
	if backend.Run[1].Name != "worker" || backend.Run[1].Cmd != "npm run start:worker" {
		t.Errorf("backend.Run[1] = %+v", backend.Run[1])
	}

	adminUI := cfg.findRepo("admin-ui")
	if adminUI == nil {
		t.Fatal("findRepo(admin-ui) = nil")
	}
	if !adminUI.IsWindows() {
		t.Error("admin-ui.IsWindows() = false, want true")
	}
	if len(adminUI.Run) != 1 || adminUI.Run[0].Name != "dev" {
		t.Errorf("adminUI.Run = %+v", adminUI.Run)
	}

	if got := cfg.findRepo("does-not-exist"); got != nil {
		t.Errorf("findRepo(does-not-exist) = %+v, want nil", got)
	}
}

const defaultsFixtureYAML = `
defaults:
  default_branch: main
  base_root: ~/dev/.fleet-base
  windows_base_root: ~/dev/.fleet-base-windows

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
    run:
      - name: dev
        cmd: "npm run dev"

  - name: room-launcher
    origin: git@github.com:org/room-launcher.git
    runtime: windows
    run:
      - name: dev
        cmd: "npm run dev"
`

func TestParseReposConfigAppliesDefaults(t *testing.T) {
	cfg, err := parseReposConfig([]byte(defaultsFixtureYAML))
	if err != nil {
		t.Fatalf("parseReposConfig: %v", err)
	}

	backend := cfg.findRepo("backend")
	if backend == nil {
		t.Fatal("findRepo(backend) = nil")
	}
	if backend.Base != "~/dev/.fleet-base/backend" {
		t.Errorf("backend.Base = %q, want default-derived path", backend.Base)
	}
	if backend.DefaultBranch != "main" {
		t.Errorf("backend.DefaultBranch = %q, want %q", backend.DefaultBranch, "main")
	}

	adminUI := cfg.findRepo("admin-ui")
	if adminUI == nil {
		t.Fatal("findRepo(admin-ui) = nil")
	}
	if adminUI.Base != "~/dev/.fleet-base/admin-ui-custom" {
		t.Errorf("adminUI.Base = %q, want explicit value preserved", adminUI.Base)
	}
	if adminUI.DefaultBranch != "develop" {
		t.Errorf("adminUI.DefaultBranch = %q, want explicit value preserved", adminUI.DefaultBranch)
	}

	if cfg.WindowsWorktreeRoot != "~/dev/.fleet-worktrees-windows" {
		t.Errorf("WindowsWorktreeRoot = %q", cfg.WindowsWorktreeRoot)
	}
	if cfg.WindowsCacheRoot != "~/dev/.fleet-cache-windows" {
		t.Errorf("WindowsCacheRoot = %q", cfg.WindowsCacheRoot)
	}

	roomLauncher := cfg.findRepo("room-launcher")
	if roomLauncher == nil {
		t.Fatal("findRepo(room-launcher) = nil")
	}
	if roomLauncher.WindowsBase != "~/dev/.fleet-base-windows/room-launcher" {
		t.Errorf("roomLauncher.WindowsBase = %q, want default-derived path", roomLauncher.WindowsBase)
	}
	if roomLauncher.Base != "" {
		t.Errorf("roomLauncher.Base = %q, want empty (windows-runtime repos don't use base_root)", roomLauncher.Base)
	}
}
