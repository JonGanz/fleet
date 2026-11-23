package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
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
// file is missing, unparsable or invalid, and every helper taking a
// *ReposConfig tolerates nil. A file that exists but can't be used is
// warned about, since the fallback then ignores its base paths.
func loadReposConfigOptional() *ReposConfig {
	cfg, _, err := loadConfig()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			warnf("ignoring repos.yaml: %v", err)
		}
		return nil
	}
	return cfg
}

// lockAndReadTask takes ticket's lock (see lockTicket) and then reads its
// state, so the state the caller modifies can't change underneath it. An
// invalid id or a ticket with no state file is a usage error (exit 2). On
// success the caller must call unlock when done; on error nothing is held.
func lockAndReadTask(ticket string) (tf string, st *TaskState, unlock func(), err error) {
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
	st, err = readTaskState(tf)
	if err != nil {
		unlock()
		return "", nil, nil, fmt.Errorf("read task %s: %w", ticket, err)
	}
	if st.Ticket != ticket {
		warnf("%s says ticket %q; using %q from its filename", tf, st.Ticket, ticket)
		st.Ticket = ticket
	}
	return tf, st, unlock, nil
}

// noTaskError is the usage error for a ticket with no state file.
func noTaskError(ticket string) error {
	return usageErrorf("no task found for ticket %s (see `fleet-task list`)", ticket)
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

// validateTicketName rejects ticket ids that can't safely become both a
// JSON filename (<tasks dir>/<ticket>.json) and a worktree directory
// component (<worktree_root>/<ticket>/<repo>) -- on Linux and, for
// runtime: windows repos, on NTFS -- or that would be misread as a flag on
// a later command line. Invalid ids are usage errors.
func validateTicketName(ticket string) error {
	const maxTicketLen = 100
	switch {
	case ticket == "":
		return usageErrorf("ticket id is required")
	case len(ticket) > maxTicketLen:
		return usageErrorf("ticket id %q is longer than %d bytes", ticket, maxTicketLen)
	case strings.ContainsAny(ticket, `/\`):
		return usageErrorf("ticket id %q must not contain path separators", ticket)
	case ticket == "." || ticket == "..":
		return usageErrorf("ticket id %q is not valid", ticket)
	case strings.HasPrefix(ticket, "-"):
		return usageErrorf("ticket id %q must not start with '-'", ticket)
	case strings.ContainsAny(ticket, `<>:"|?*`):
		return usageErrorf("ticket id %q must not contain any of <>:\"|?* (not allowed in Windows filenames)", ticket)
	case strings.HasSuffix(ticket, ".") || strings.HasSuffix(ticket, " "):
		return usageErrorf("ticket id %q must not end with '.' or a space (not allowed in Windows filenames)", ticket)
	}
	for _, r := range ticket {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == utf8.RuneError {
			return usageErrorf("ticket id %q must not contain whitespace, control characters or invalid UTF-8", ticket)
		}
	}
	return nil
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
