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
