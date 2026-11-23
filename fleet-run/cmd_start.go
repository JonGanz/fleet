package main

import (
	"fmt"
	"os"
	"strings"
)

// startOpts are `fleet-run start`'s parsed flags and arguments.
type startOpts struct {
	all     bool
	asJSON  bool
	targets []string // "repo:run-name" labels
	ticket  string
}

// runStart implements `fleet-run start [--ticket <id>] [--all]
// [repo:run-name ...] [--json]`. Only one ticket's windows are ever meant to
// be live in the session at once, so starting a different ticket than
// whatever's currently active tears down every existing window first.
// Starting the *same* ticket that's already active stays additive (only
// missing windows get created; dead panes get respawned).
func runStart(opts startOpts) error {
	if opts.all && len(opts.targets) > 0 {
		return usageErrorf("--all and explicit repo:run-name targets are mutually exclusive")
	}
	if !opts.all && len(opts.targets) == 0 && !interactiveAllowed() {
		return inputRequiredError("no targets given; pass repo:run-name ... or --all (see `fleet-run targets`)")
	}
	cfg, ts, err := loadStartContext(opts.ticket)
	if err != nil {
		return err
	}
	return startFlow(cfg, ts, opts)
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
	if !interactiveAllowed() {
		return "", inputRequiredError(fmt.Sprintf("multiple tasks exist (%s); pass --ticket", strings.Join(ticketsFromFiles(files), ", ")))
	}

	chosen, err := selectOne(ticketsFromFiles(files))
	if err != nil {
		return "", fmt.Errorf("select ticket: %w", err)
	}
	return chosen, nil
}

// startFlow resolves which targets to start (explicit, --all, or the
// multiselect), switches tickets if needed, and creates or respawns each
// target's window. It attempts every target, then returns an error if any
// failed (so the exit code is non-zero on partial failure). With asJSON the
// StartReport is printed to stdout either way.
func startFlow(cfg *ReposConfig, ts *TaskState, opts startOpts) error {
	session, err := requireSession(cfg)
	if err != nil {
		return err
	}
	pairs := availablePairs(cfg, ts)
	if len(pairs) == 0 {
		return fmt.Errorf("no repo:run-name targets available for ticket %s (check repos.yaml `run` entries against the task's worktrees)", ts.Ticket)
	}

	selected, err := selectStartTargets(pairs, opts)
	if err != nil {
		return err
	}
	report := StartReport{Ticket: ts.Ticket, Results: []StartResult{}}
	if len(selected) == 0 {
		// Deliberately does nothing else -- no session/option changes -- so
		// backing out of selection never tears down a working environment.
		fmt.Fprintln(os.Stderr, "no targets selected, nothing to start")
		return finishStart(report, opts.asJSON)
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
		report.SwitchedFrom = active
		// Killing every window -- including the last one -- makes tmux
		// destroy the session itself (and the server too, if it was the only
		// session); the newWindow call below recreates it together with
		// whichever window gets created first. If the server survived, the
		// old ticket's record is stale now, so drop it.
		if err := clearActiveTicket(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: clearing stale active ticket record: %v\n", err)
		}
	}

	panes, err := listPanes(session)
	if err != nil {
		return err
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
	if len(panes) > 0 {
		if err := record(); err != nil {
			return err
		}
	}

	for _, pair := range selected {
		res := startOne(session, cfg.Tmux.ConfigFile, pair, panes)
		if res.Status == startStatusStarted || res.Status == startStatusRespawned {
			if err := record(); err != nil {
				res.Status, res.Error = startStatusFailed, err.Error()
			}
		}
		if res.Status == startStatusFailed {
			fmt.Fprintf(os.Stderr, "warning: failed to start %s: %s\n", res.Window, res.Error)
		} else {
			fmt.Fprintf(os.Stderr, "%s %s\n", res.Status, res.Window)
		}
		report.Results = append(report.Results, res)
	}
	return finishStart(report, opts.asJSON)
}

// finishStart prints report (with asJSON) and turns failures into an error.
func finishStart(report StartReport, asJSON bool) error {
	failed := report.failedCount()
	report.OK = failed == 0
	if asJSON {
		if err := printJSON(report); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d selected targets failed to start", failed, len(report.Results))
	}
	return nil
}

// selectStartTargets returns the pairs to start: explicit labels or --all
// without a picker, else the interactive multiselect (runStart has already
// refused the no-terminal case).
func selectStartTargets(pairs []RunPair, opts startOpts) ([]RunPair, error) {
	selected, needPicker, err := resolveStartTargets(pairs, opts.all, opts.targets)
	if err != nil || !needPicker {
		return selected, err
	}
	if !interactiveAllowed() {
		return nil, inputRequiredError("no targets given; pass repo:run-name ... or --all (see `fleet-run targets`)")
	}
	labels := make([]string, len(pairs))
	for i, p := range pairs {
		labels[i] = p.Label()
	}
	chosen, err := selectMulti(labels)
	if err != nil {
		return nil, err
	}
	for _, label := range chosen {
		if pair, ok := findPairByLabel(pairs, label); ok {
			selected = append(selected, pair)
		}
	}
	return selected, nil
}

// startOne creates pair's window, respawns it if its pane is dead, or leaves
// a live one alone.
func startOne(session, configFile string, pair RunPair, panes []PaneInfo) StartResult {
	res := StartResult{Target: pair.Label(), Window: pair.WindowName()}
	if pane, exists := findPane(panes, res.Window); exists {
		res.Status = existingWindowStatus(pane)
		if res.Status == startStatusRespawned {
			if err := respawnWindow(session, res.Window, pair); err != nil {
				res.Status, res.Error = startStatusFailed, err.Error()
			}
		}
		return res
	}
	if err := newWindow(session, configFile, res.Window, pair); err != nil {
		res.Status, res.Error = startStatusFailed, err.Error()
		return res
	}
	res.Status = startStatusStarted
	return res
}
