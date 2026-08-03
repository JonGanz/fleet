package main

import (
	"fmt"
	"os"
)

// runStop implements `fleet-run stop [--ticket <id>] [--all] [--everything] [names...]`.
func runStop(ticket string, all, everything bool, names []string) error {
	rf, err := reposFile()
	if err != nil {
		return err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return err
	}
	session := cfg.Tmux.SessionName
	if session == "" {
		return fmt.Errorf("repos.yaml tmux.session_name is required")
	}

	windows, err := listWindows(session)
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		fmt.Fprintln(os.Stderr, "no windows currently running")
		return nil
	}

	var toKill []string

	switch {
	case all:
		toKill, err = filterAllTarget(windows, ticket, everything)
		if err != nil {
			return err
		}

	case len(names) > 0:
		resolveTicket := ticket
		if resolveTicket == "" {
			td, err := tasksDir()
			if err != nil {
				return err
			}
			ts, err := resolveTicketState(td, "")
			if err != nil {
				return fmt.Errorf("resolving ticket for positional names: %w", err)
			}
			resolveTicket = ts.Ticket
		}
		toKill, err = namesToWindowNames(names, resolveTicket)
		if err != nil {
			return err
		}

	default:
		if ticket == "" {
			fmt.Fprintln(os.Stderr, "warning: no --ticket given, selecting from windows across all tickets")
		}
		candidates := candidateWindowsForStop(windows, ticket)
		if len(candidates) == 0 {
			fmt.Fprintln(os.Stderr, "no matching windows to stop")
			return nil
		}
		selected, err := multiSelect(candidates)
		if err != nil {
			return err
		}
		toKill = selected
	}

	if len(toKill) == 0 {
		fmt.Fprintln(os.Stderr, "nothing selected to stop")
		return nil
	}

	runningSet := make(map[string]bool, len(windows))
	for _, w := range windows {
		runningSet[w] = true
	}

	for _, wname := range toKill {
		if !runningSet[wname] {
			fmt.Fprintf(os.Stderr, "warning: window %q is not running, skipping\n", wname)
			continue
		}
		if err := killWindow(session, wname); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to kill window %q: %v\n", wname, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "stopped %s\n", wname)
	}
	return nil
}

// filterAllTarget wraps validateAllFlags + filterWindowsByTicketPrefix for
// the --all case.
func filterAllTarget(windows []string, ticket string, everything bool) ([]string, error) {
	if err := validateAllFlags(ticket, everything); err != nil {
		return nil, err
	}
	return filterWindowsByTicketPrefix(windows, ticket), nil
}

// killAllWindowsInSession kills every window currently in the session,
// unconditionally. Used internally by `switch`, which is defined as
// stopping *everything* currently running (only one ticket's app set is
// meant to be live at a time) before starting the new ticket's selection.
func killAllWindowsInSession(session string) error {
	windows, err := listWindows(session)
	if err != nil {
		return err
	}
	for _, wname := range windows {
		if err := killWindow(session, wname); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to kill window %q: %v\n", wname, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "stopped %s\n", wname)
	}
	return nil
}
