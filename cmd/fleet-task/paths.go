package main

import (
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/taskstate"
	"github.com/JonGanz/fleet/internal/xdg"
)

// taskFile returns <state dir>/tasks/<ticket>.json. The ticket id is
// validated first (see taskstate.ValidateTicket), so every command that
// accepts a ticket from user input -- new/edit/rm/mv/jump -- is protected
// against ids like "a/b" or ".." escaping the tasks dir by construction.
func taskFile(ticket string) (string, error) {
	if err := taskstate.ValidateTicket(ticket); err != nil {
		return "", err
	}
	td, err := xdg.TasksDir()
	if err != nil {
		return "", err
	}
	return taskstate.Path(td, ticket)
}

// worktreeRoot returns the effective worktree root: repos.yaml's
// worktree_root if set, else the default state-dir location.
func worktreeRoot(cfg *config.ReposConfig) (string, error) {
	if cfg != nil && cfg.WorktreeRoot != "" {
		return xdg.ExpandHome(cfg.WorktreeRoot), nil
	}
	return xdg.DefaultWorktreeRoot()
}
