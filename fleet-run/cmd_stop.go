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
	// Stopped, NotRunning and Failed hold window names. Each *Targets field
	// is parallel to its window-name field and holds the "repo:run-name"
	// form, or "" for a window repos.yaml no longer describes.
	Stopped           []string `json:"stopped"`
	StoppedTargets    []string `json:"stopped_targets"`
	NotRunning        []string `json:"not_running"`
	NotRunningTargets []string `json:"not_running_targets"`
	Failed            []string `json:"failed"`
	FailedTargets     []string `json:"failed_targets"`
	// ClearedActive is true when nothing was left running, so the
	// active-ticket record was cleared (also when it was a stale record
	// left behind by a session killed outside fleet-run).
	ClearedActive bool `json:"cleared_active"`
}

// newStopReport returns a report whose arrays are all non-nil, so they
// encode as [] rather than null.
func newStopReport() StopReport {
	return StopReport{
		Stopped: []string{}, StoppedTargets: []string{},
		NotRunning: []string{}, NotRunningTargets: []string{},
		Failed: []string{}, FailedTargets: []string{},
	}
}

// labelsFor maps window names to "repo:run-name" labels ("" when unknown).
func labelsFor(windows []string, labels map[string]string) []string {
	out := make([]string, len(windows))
	for i, w := range windows {
		out[i] = labels[w]
	}
	return out
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
	if opts.ticket != "" {
		if err := validateTicket(opts.ticket); err != nil {
			return err
		}
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
		active, hasActive, err := activeTicket()
		if err != nil {
			return err
		}
		if err := checkTicketMatchesActive(opts.ticket, active, hasActive); err != nil {
			return err
		}
	}

	report := newStopReport()
	windows, err := listWindows(session)
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		// Checked before the input-required check below so a bare `stop`
		// also clears a stale record: with nothing running there is nothing
		// to pick, and the record (left behind when the session was killed
		// outside fleet-run while the server survived) would otherwise make
		// fleet-task refuse to remove the ticket forever.
		fmt.Fprintln(os.Stderr, "no windows currently running")
		report.NotRunning = append(report.NotRunning, named...)
		report.ClearedActive = clearStaleActive()
		return finishStop(report, windowLabels(cfg), opts.asJSON)
	}
	if !opts.all && len(named) == 0 && !interactiveAllowed() {
		return inputRequiredError("no targets given; pass repo:run-name ... or --all (see `fleet-run status`)")
	}

	names := windowNames(windows)
	var toKill []string
	switch {
	case opts.all:
		toKill = names
	case len(named) > 0:
		toKill = named
	default:
		// Show the same "repo:run" form the positional args take; window
		// names themselves stay "repo-run".
		labels := windowLabels(cfg)
		selected, err := selectMulti(pickerItems(names, labels))
		if err != nil {
			return err
		}
		toKill = pickerSelectionToWindows(selected, names, labels)
	}

	if len(toKill) == 0 {
		fmt.Fprintln(os.Stderr, "nothing selected to stop")
		return finishStop(report, windowLabels(cfg), opts.asJSON)
	}

	var running []tmuxWindow
	for _, wname := range uniqueStrings(toKill) {
		matches := windowsNamed(windows, wname)
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "warning: window %q is not running, skipping\n", wname)
			report.NotRunning = append(report.NotRunning, wname)
			continue
		}
		running = append(running, matches...)
	}
	stopped, failed := killWindows(running)
	report.Stopped = append(report.Stopped, stopped...)
	report.Failed = append(report.Failed, failed...)

	remaining, err := listWindows(session)
	if err != nil {
		return err
	}
	if shouldClearActive(windowNames(remaining)) {
		if err := clearActiveTicket(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: clearing active ticket record: %v\n", err)
		} else {
			report.ClearedActive = true
		}
	}
	return finishStop(report, windowLabels(cfg), opts.asJSON)
}

// clearStaleActive clears an active-ticket record when no windows are
// running, reporting whether there was one to clear.
func clearStaleActive() bool {
	active, hasActive, err := activeTicket()
	if err != nil || !hasActive {
		return false
	}
	if err := clearActiveTicket(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: clearing stale active ticket record for %s: %v\n", active, err)
		return false
	}
	fmt.Fprintf(os.Stderr, "cleared stale active ticket record for %s\n", active)
	return true
}

// finishStop fills in report's *Targets fields and OK, prints it (with
// asJSON) and turns failures into an error.
func finishStop(report StopReport, labels map[string]string, asJSON bool) error {
	report.StoppedTargets = labelsFor(report.Stopped, labels)
	report.NotRunningTargets = labelsFor(report.NotRunning, labels)
	report.FailedTargets = labelsFor(report.Failed, labels)
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

// killWindows kills each window (by id), reporting progress on stderr, and
// returns the names of those whose kill succeeded and failed. Shared by
// `stop` and the switch path in `start`.
func killWindows(windows []tmuxWindow) (stopped, failed []string) {
	for _, w := range windows {
		if err := killWindow(w.ID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to kill window %q: %v\n", w.Name, err)
			failed = append(failed, w.Name)
			continue
		}
		fmt.Fprintf(os.Stderr, "stopped %s\n", w.Name)
		stopped = append(stopped, w.Name)
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
	if _, failed := killWindows(windows); len(failed) > 0 {
		return fmt.Errorf("%d window(s) failed to stop while switching tickets", len(failed))
	}
	return nil
}
