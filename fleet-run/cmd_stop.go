package main

import (
	"fmt"
	"os"
)

// runStop implements `fleet-run stop [--ticket <id>] [--all] [names...]`.
func runStop(ticket string, all bool, names []string) error {
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

	if ticket != "" {
		active, hasActive, err := activeTicket()
		if err != nil {
			return err
		}
		if err := checkTicketMatchesActive(ticket, active, hasActive); err != nil {
			return err
		}
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
		toKill = windows

	case len(names) > 0:
		toKill, err = namesToWindowNames(names)
		if err != nil {
			return err
		}

	default:
		selected, err := selectMulti(windows)
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

	remaining, err := listWindows(session)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		if err := clearActiveTicket(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: clearing active ticket record: %v\n", err)
		}
	}
	return nil
}

// killAllWindowsInSession kills every window currently in the session,
// unconditionally. Used internally by `start` when switching to a
// different ticket than whatever's currently active (only one ticket's app
// set is meant to be live at a time).
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
