package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/taskstate"
	"github.com/JonGanz/fleet/internal/xdg"
)

// requireTaskExists returns noTaskError if tf (ticket's state file) is
// missing.
func requireTaskExists(ticket, tf string) error {
	if _, err := os.Stat(tf); os.IsNotExist(err) {
		return noTaskError(ticket)
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
func loadConfig() (*config.ReposConfig, string, error) {
	rf, err := xdg.ReposFile()
	if err != nil {
		return nil, "", err
	}
	cfg, err := config.Load(rf)
	if err != nil {
		return nil, rf, err
	}
	return cfg, rf, nil
}

// loadReposConfigOptional is loadConfig for commands (rm, mv) that can
// still do useful best-effort work without repos.yaml: it returns nil if the
// file is missing, unparsable or invalid, and every helper taking a
// *config.ReposConfig tolerates nil. A file that exists but can't be used is
// warned about, since the fallback then ignores its base paths.
func loadReposConfigOptional() *config.ReposConfig {
	cfg, _, err := loadConfig()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			cli.Warnf("ignoring repos.yaml: %v", err)
		}
		return nil
	}
	return cfg
}

// lockAndReadTask takes ticket's lock (see lockTicket) and then reads its
// state, so the state the caller modifies can't change underneath it. An
// invalid id or a ticket with no state file is a usage error (exit 2). On
// success the caller must call unlock when done; on error nothing is held.
func lockAndReadTask(ticket string) (tf string, st *taskstate.State, unlock func(), err error) {
	tf, err = taskFile(ticket)
	if err != nil {
		return "", nil, nil, err
	}
	if err := requireTaskExists(ticket, tf); err != nil {
		return "", nil, nil, err
	}
	unlock, err = lockTicket(tf)
	if err != nil {
		return "", nil, nil, err
	}
	// Re-checked under the lock: a concurrent rm/mv may have just removed it.
	if err := requireTaskExists(ticket, tf); err != nil {
		unlock()
		return "", nil, nil, err
	}
	st, err = taskstate.Read(tf)
	if err != nil {
		unlock()
		return "", nil, nil, fmt.Errorf("read task %s: %w", ticket, err)
	}
	return tf, st, unlock, nil
}

// noTaskError is the usage error for a ticket with no state file.
func noTaskError(ticket string) error {
	return cli.Usagef("no task found for ticket %s (see `fleet-task list`)", ticket)
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

// uniqueStrings returns items without duplicates, keeping first-seen order.
func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	var unique []string
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			unique = append(unique, it)
		}
	}
	return unique
}
