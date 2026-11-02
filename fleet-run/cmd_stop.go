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
	session, err := requireSession(cfg)
	if err != nil {
		return err
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
		// Show the same "repo:run" form the positional args take; window
		// names themselves stay "repo-run".
		labels := windowLabels(cfg)
		selected, err := selectMulti(pickerItems(windows, labels))
		if err != nil {
			return err
		}
		toKill = pickerSelectionToWindows(selected, windows, labels)
	}

	if len(toKill) == 0 {
		fmt.Fprintln(os.Stderr, "nothing selected to stop")
		return nil
	}

	runningSet := make(map[string]bool, len(windows))
	for _, w := range windows {
		runningSet[w] = true
	}
	var running []string
	for _, wname := range toKill {
		if !runningSet[wname] {
			fmt.Fprintf(os.Stderr, "warning: window %q is not running, skipping\n", wname)
			continue
		}
		running = append(running, wname)
	}
	failed := killWindows(session, running)

	remaining, err := listWindows(session)
	if err != nil {
		return err
	}
	if shouldClearActive(remaining) {
		if err := clearActiveTicket(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: clearing active ticket record: %v\n", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d window(s) failed to stop", failed)
	}
	return nil
}

// killWindows kills each named window, reporting progress on stderr, and
// returns how many kills failed. Shared by `stop` and the switch path in
// `start`.
func killWindows(session string, names []string) (failed int) {
	for _, wname := range names {
		if err := killWindow(session, wname); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to kill window %q: %v\n", wname, err)
			failed++
			continue
		}
		fmt.Fprintf(os.Stderr, "stopped %s\n", wname)
	}
	return failed
}

// killAllWindowsInSession kills every window currently in the session,
// unconditionally. Used internally by `start` when switching to a
// different ticket than whatever's currently active (only one ticket's app
// set is meant to be live at a time). Errors if any window couldn't be
// killed, so a switch never proceeds on top of the old ticket's windows.
func killAllWindowsInSession(session string) error {
	windows, err := listWindows(session)
	if err != nil {
		return err
	}
	if failed := killWindows(session, windows); failed > 0 {
		return fmt.Errorf("%d window(s) failed to stop while switching tickets", failed)
	}
	return nil
}
