package main

import (
	"os"
	"path/filepath"
	"testing"
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

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", p, err)
	}
	return p
}

func TestLoadReposConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "repos.yaml", fixtureYAML)

	cfg, err := loadReposConfig(p)
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

	if got := cfg.findRepo("backend"); got == nil || got.Name != "backend" {
		t.Errorf("findRepo(backend) = %+v", got)
	}
	if got := cfg.findRepo("nonexistent"); got != nil {
		t.Errorf("findRepo(nonexistent) = %+v, want nil", got)
	}

	names := cfg.repoNames()
	if len(names) != 2 || names[0] != "backend" || names[1] != "admin-ui" {
		t.Errorf("repoNames() = %v", names)
	}
}

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

func TestLoadReposConfigAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "repos.yaml", defaultsFixtureYAML)

	cfg, err := loadReposConfig(p)
	if err != nil {
		t.Fatalf("loadReposConfig: %v", err)
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
	if backend.BranchTemplate != "eng/{ticket}" {
		t.Errorf("backend.BranchTemplate = %q, want default-derived %q", backend.BranchTemplate, "eng/{ticket}")
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
	if adminUI.BranchTemplate != "ui/{ticket}" {
		t.Errorf("adminUI.BranchTemplate = %q, want explicit value preserved", adminUI.BranchTemplate)
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

	staffQuiosk := cfg.findRepo("staff-quiosk")
	if staffQuiosk == nil {
		t.Fatal("findRepo(staff-quiosk) = nil")
	}
	if staffQuiosk.WindowsBase != "~/dev/.fleet-base-windows/staff-quiosk-custom" {
		t.Errorf("staffQuiosk.WindowsBase = %q, want explicit value preserved", staffQuiosk.WindowsBase)
	}
}

func TestCheckSameDrive(t *testing.T) {
	if err := checkSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": `C:\fleet\worktrees`,
	}); err != nil {
		t.Errorf("checkSameDrive same-drive: %v", err)
	}

	if err := checkSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": `D:\fleet\worktrees`,
	}); err == nil {
		t.Error("checkSameDrive cross-drive: want error, got nil")
	}

	if err := checkSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": "",
	}); err != nil {
		t.Errorf("checkSameDrive with blank entry: %v", err)
	}
}
