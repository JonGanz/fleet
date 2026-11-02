package main

import (
	"fmt"
	"os"
)

// runStart implements `fleet-run start [--ticket <id>]`. Only one ticket's
// windows are ever meant to be live in the session at once, so starting a
// different ticket than whatever's currently active tears down every
// existing window first -- this is what used to be the separate `switch`
// command; it's now just what `start` does when the ticket changes.
// Starting the *same* ticket that's already active stays additive (only
// missing windows get created), matching how reruns always worked.
func runStart(ticket string) error {
	cfg, ts, err := loadStartContext(ticket)
	if err != nil {
		return err
	}
	return startFlow(cfg, ts)
}

// requireSession returns the configured tmux session name, erroring if it's
// missing. Called before any picker so misconfiguration fails fast.
func requireSession(cfg *ReposConfig) (string, error) {
	if cfg.Tmux.SessionName == "" {
		return "", fmt.Errorf("repos.yaml tmux.session_name is required")
	}
	return cfg.Tmux.SessionName, nil
}

// loadStartContext loads repos.yaml and resolves the task state to operate
// on.
func loadStartContext(ticket string) (*ReposConfig, *TaskState, error) {
	rf, err := reposFile()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return nil, nil, err
	}

	if _, err := requireSession(cfg); err != nil {
		return nil, nil, err
	}

	td, err := tasksDir()
	if err != nil {
		return nil, nil, err
	}
	if ticket == "" {
		ticket, err = pickTicketIfAmbiguous(td)
		if err != nil {
			return nil, nil, err
		}
	}
	ts, err := resolveTicketState(td, ticket)
	if err != nil {
		return nil, nil, err
	}
	return cfg, ts, nil
}

// pickTicketIfAmbiguous leaves ticket selection to resolveTicketState's own
// zero/one-file handling when there's nothing to choose between, and only
// steps in when there are multiple task files and no --ticket was given --
// in that case it launches the bubbletea single-select picker over ticket
// names so `start` with no arguments works interactively instead of
// erroring and telling the user to pass --ticket.
func pickTicketIfAmbiguous(tasksDirPath string) (string, error) {
	files, err := listTaskFiles(tasksDirPath)
	if err != nil {
		return "", fmt.Errorf("list task files in %s: %w", tasksDirPath, err)
	}
	if len(files) <= 1 {
		return "", nil
	}

	chosen, err := selectOne(ticketsFromFiles(files))
	if err != nil {
		return "", fmt.Errorf("select ticket: %w", err)
	}
	return chosen, nil
}

// startFlow runs the interactive multiselect + tmux window creation flow
// for the given already-resolved config/task. It attempts every selected
// pair, then returns an error if any of them failed to start (so the exit
// code is non-zero on partial failure).
func startFlow(cfg *ReposConfig, ts *TaskState) error {
	session, err := requireSession(cfg)
	if err != nil {
		return err
	}
	pairs := availablePairs(cfg, ts)
	if len(pairs) == 0 {
		return fmt.Errorf("no repo:run-name targets available for ticket %s (check repos.yaml `run` entries against the task's worktrees)", ts.Ticket)
	}

	labels := make([]string, len(pairs))
	for i, p := range pairs {
		labels[i] = p.Label()
	}

	selected, err := selectMulti(labels)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		// Deliberately does nothing else -- no session/option changes -- so
		// backing out of selection never tears down a working environment.
		fmt.Fprintln(os.Stderr, "no targets selected, nothing to start")
		return nil
	}

	active, hasActive, err := activeTicket()
	if err != nil {
		return err
	}
	if needsSwitch(active, hasActive, ts.Ticket) {
		fmt.Fprintf(os.Stderr, "switching from %s to %s: stopping all currently running windows\n", active, ts.Ticket)
		if err := killAllWindowsInSession(session); err != nil {
			return err
		}
		// Killing every window -- including the last one -- makes tmux
		// destroy the session itself (and the server too, if it was the only
		// session); the newWindow call below recreates it together with
		// whichever window gets created first. If the server survived, the
		// old ticket's record is stale now, so drop it.
		if err := clearActiveTicket(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: clearing stale active ticket record: %v\n", err)
		}
	}

	existing, err := listWindows(session)
	if err != nil {
		return err
	}
	existingSet := make(map[string]bool, len(existing))
	for _, w := range existing {
		existingSet[w] = true
	}

	// The active-ticket options are tmux server options, and `tmux
	// set-option` does not start a server, so they can only be set once a
	// window (hence a server) exists: right after the first successful
	// creation, or immediately if the session already has windows.
	recorded := false
	record := func() error {
		if recorded {
			return nil
		}
		recorded = true
		if err := setActiveTicket(ts); err != nil {
			return fmt.Errorf("recording active ticket: %w", err)
		}
		return nil
	}
	if len(existingSet) > 0 {
		if err := record(); err != nil {
			return err
		}
	}

	failed := 0
	for _, label := range selected {
		pair, ok := findPairByLabel(pairs, label)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: unrecognized selection %q, skipping\n", label)
			failed++
			continue
		}
		wname := pair.WindowName()
		if existingSet[wname] {
			fmt.Fprintf(os.Stderr, "warning: window %q already exists, skipping\n", wname)
			continue
		}
		if err := newWindow(session, cfg.Tmux.ConfigFile, wname, pair); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to start window %q: %v\n", wname, err)
			failed++
			continue
		}
		existingSet[wname] = true
		fmt.Fprintf(os.Stderr, "started %s\n", wname)
		if err := record(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d selected targets failed to start", failed, len(selected))
	}
	return nil
}
