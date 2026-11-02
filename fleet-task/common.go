package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateTicketName rejects ticket ids that can't safely become both a
// JSON filename (<tasks dir>/<ticket>.json) and a worktree directory
// component (<worktree_root>/<ticket>/<repo>).
func validateTicketName(ticket string) error {
	if ticket == "" {
		return fmt.Errorf("ticket id is required")
	}
	if ticket != filepath.Base(ticket) || strings.Contains(ticket, "/") || strings.Contains(ticket, `\`) {
		return fmt.Errorf("ticket id %q must not contain path separators", ticket)
	}
	if ticket == "." || ticket == ".." {
		return fmt.Errorf("ticket id %q is not valid", ticket)
	}
	return nil
}

// requireNotActive refuses to proceed if ticket is the one fleet-run
// currently has live. Tearing down or renaming a worktree out from under a
// still-running app is what produces the "Directory not empty" mess
// selfHealWorktreeRemove papers over (a live dev server can be writing into
// its own untracked cache dir at the exact moment git deletes it), and
// renaming a live ticket would desync fleet-run's tmux @fleet_task_ticket
// option from the new filename. action completes the sentence "...before
// <action>", e.g. "removing it".
func requireNotActive(ticket, action string) error {
	if active, found := activeFleetTicket(); found && active == ticket {
		return fmt.Errorf("%s is currently active in fleet-run; run `fleet-run stop --ticket %s` (or `fleet-run stop --all`) before %s", ticket, ticket, action)
	}
	return nil
}

// loadConfig locates and parses repos.yaml, returning its path alongside the
// parsed config for use in error messages.
func loadConfig() (*ReposConfig, string, error) {
	rf, err := reposFile()
	if err != nil {
		return nil, "", err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return nil, rf, err
	}
	return cfg, rf, nil
}

// loadReposConfigOptional is loadConfig for commands (rm, mv) that can
// still do useful best-effort work without repos.yaml: it returns nil if the
// file is missing or unparsable, and every helper taking a *ReposConfig
// tolerates nil.
func loadReposConfigOptional() *ReposConfig {
	cfg, _, err := loadConfig()
	if err != nil {
		return nil
	}
	return cfg
}

// removeDirIfEmpty removes dir only if it has no entries, and otherwise
// leaves it alone and returns the names of what's inside. A missing dir is
// not an error. This is the safe replacement for os.RemoveAll on a ticket
// workspace dir, which may hold user files (notes, env files) beside the
// worktrees.
func removeDirIfEmpty(dir string) (removed bool, leftovers []string, err error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if len(entries) > 0 {
		for _, e := range entries {
			leftovers = append(leftovers, e.Name())
		}
		return false, leftovers, nil
	}
	if err := os.Remove(dir); err != nil {
		return false, nil, err
	}
	return true, nil, nil
}

// warnf prints a "warning: ..." line to stderr; the one spelling used for
// non-fatal problems throughout fleet-task (fatal ones are returned as
// errors and printed as "error: ..." by main).
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: "+format+"\n", args...)
}

// errorf prints an "error: ..." line to stderr without aborting, for
// per-repo failures that are reported while the rest of the repos are still
// attempted.
func errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
}
