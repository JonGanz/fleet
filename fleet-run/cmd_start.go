package main

import (
	"fmt"
	"os"
)

// runStart implements `fleet-run start [--ticket <id>]`.
func runStart(ticket string) error {
	cfg, ts, err := loadStartContext(ticket)
	if err != nil {
		return err
	}
	return startFlow(cfg, ts)
}

// loadStartContext loads repos.yaml and resolves the task state to operate
// on, shared by `start` and `switch`.
func loadStartContext(ticket string) (*ReposConfig, *TaskState, error) {
	rf, err := reposFile()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return nil, nil, err
	}

	td, err := tasksDir()
	if err != nil {
		return nil, nil, err
	}
	ts, err := resolveTicketState(td, ticket)
	if err != nil {
		return nil, nil, err
	}
	return cfg, ts, nil
}

// startFlow runs the interactive multiselect + tmux window creation flow
// for the given already-resolved config/task. Exposed separately from
// runStart so `switch` can call it directly rather than shelling out to its
// own binary.
func startFlow(cfg *ReposConfig, ts *TaskState) error {
	pairs := availablePairs(cfg, ts)
	if len(pairs) == 0 {
		return fmt.Errorf("no repo:run-name targets available for ticket %s (check repos.yaml `run` entries against the task's worktrees)", ts.Ticket)
	}

	labels := make([]string, len(pairs))
	for i, p := range pairs {
		labels[i] = p.Label()
	}

	selected, err := multiSelect(labels)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Fprintln(os.Stderr, "no targets selected, nothing to start")
		return nil
	}

	session := cfg.Tmux.SessionName
	if session == "" {
		return fmt.Errorf("repos.yaml tmux.session_name is required")
	}
	if err := ensureSession(session, cfg.Tmux.ConfigFile); err != nil {
		return err
	}

	existing, err := listWindows(session)
	if err != nil {
		return err
	}
	existingSet := make(map[string]bool, len(existing))
	for _, w := range existing {
		existingSet[w] = true
	}

	for _, label := range selected {
		pair, ok := findPairByLabel(pairs, label)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: unrecognized selection %q, skipping\n", label)
			continue
		}
		wname := pair.WindowName(ts.Ticket)
		if existingSet[wname] {
			fmt.Fprintf(os.Stderr, "warning: window %q already exists, skipping\n", wname)
			continue
		}
		if err := newWindow(session, wname, pair); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to start window %q: %v\n", wname, err)
			continue
		}
		existingSet[wname] = true
		fmt.Fprintf(os.Stderr, "started %s\n", wname)
	}
	return nil
}
