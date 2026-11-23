package main

import (
	"fmt"
	"os"
)

// stopOpts are `fleet-run stop`'s parsed flags and arguments.
type stopOpts struct {
	all    bool
	asJSON bool
	names  []string // "repo:run-name" labels
	ticket string
}

// StopReport is the `fleet-run stop --json` output.
type StopReport struct {
	OK bool `json:"ok"`
	// Stopped, NotRunning and Failed hold window names.
	Stopped    []string `json:"stopped"`
	NotRunning []string `json:"not_running"`
	Failed     []string `json:"failed"`
	// ClearedActive is true when nothing was left running, so the
	// active-ticket record was cleared.
	ClearedActive bool `json:"cleared_active"`
}

// runStop implements `fleet-run stop [--ticket <id>] [--all]
// [repo:run-name ...] [--json]`.
func runStop(opts stopOpts) error {
	if opts.all && len(opts.names) > 0 {
		return usageErrorf("--all and explicit repo:run-name targets are mutually exclusive")
	}
	var named []string
	if len(opts.names) > 0 {
		var err error
		if named, err = namesToWindowNames(opts.names); err != nil {
			return &usageError{msg: err.Error()}
		}
	}
	if !opts.all && len(named) == 0 && !interactiveAllowed() {
		return inputRequiredError("no targets given; pass repo:run-name ... or --all (see `fleet-run status`)")
	}

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

	if opts.ticket != "" {
		if err := validateTicket(opts.ticket); err != nil {
			return err
		}
		active, hasActive, err := activeTicket()
		if err != nil {
			return err
		}
		if err := checkTicketMatchesActive(opts.ticket, active, hasActive); err != nil {
			return err
		}
	}

	report := StopReport{Stopped: []string{}, NotRunning: []string{}, Failed: []string{}}
	windows, err := listWindows(session)
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		fmt.Fprintln(os.Stderr, "no windows currently running")
		return finishStop(report, opts.asJSON)
	}

	var toKill []string
	switch {
	case opts.all:
		toKill = windows
	case len(named) > 0:
		toKill = named
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
		return finishStop(report, opts.asJSON)
	}

	runningSet := make(map[string]bool, len(windows))
	for _, w := range windows {
		runningSet[w] = true
	}
	var running []string
	for _, wname := range toKill {
		if !runningSet[wname] {
			fmt.Fprintf(os.Stderr, "warning: window %q is not running, skipping\n", wname)
			report.NotRunning = append(report.NotRunning, wname)
			continue
		}
		running = append(running, wname)
	}
	stopped, failed := killWindows(session, running)
	report.Stopped = append(report.Stopped, stopped...)
	report.Failed = append(report.Failed, failed...)

	remaining, err := listWindows(session)
	if err != nil {
		return err
	}
	if shouldClearActive(remaining) {
		if err := clearActiveTicket(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: clearing active ticket record: %v\n", err)
		} else {
			report.ClearedActive = true
		}
	}
	return finishStop(report, opts.asJSON)
}

// finishStop prints report (with asJSON) and turns failures into an error.
func finishStop(report StopReport, asJSON bool) error {
	report.OK = len(report.Failed) == 0
	if asJSON {
		if err := printJSON(report); err != nil {
			return err
		}
	}
	if len(report.Failed) > 0 {
		return fmt.Errorf("%d window(s) failed to stop", len(report.Failed))
	}
	return nil
}

// killWindows kills each named window, reporting progress on stderr, and
// returns which kills succeeded and which failed. Shared by `stop` and the
// switch path in `start`.
func killWindows(session string, names []string) (stopped, failed []string) {
	for _, wname := range names {
		if err := killWindow(session, wname); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to kill window %q: %v\n", wname, err)
			failed = append(failed, wname)
			continue
		}
		fmt.Fprintf(os.Stderr, "stopped %s\n", wname)
		stopped = append(stopped, wname)
	}
	return stopped, failed
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
	if _, failed := killWindows(session, windows); len(failed) > 0 {
		return fmt.Errorf("%d window(s) failed to stop while switching tickets", len(failed))
	}
	return nil
}
