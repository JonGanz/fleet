package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/hooks"
	"github.com/JonGanz/fleet/internal/taskstate"
	"github.com/JonGanz/fleet/internal/xdg"
)

// startOpts are `fleet-run start`'s parsed flags and arguments.
type startOpts struct {
	all    bool
	asJSON bool
	// restart makes a target whose app is still alive restart
	// (`fleet-run restart`) instead of being left alone.
	restart bool
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
		return cli.Usagef("--all and explicit repo:run-name targets are mutually exclusive")
	}
	if !opts.all && len(opts.targets) == 0 && !cli.InteractiveAllowed() {
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
func requireSession(cfg *config.ReposConfig) (string, error) {
	if cfg.Tmux.SessionName == "" {
		return "", fmt.Errorf("repos.yaml tmux.session_name is required")
	}
	return cfg.Tmux.SessionName, nil
}

// loadStartContext loads repos.yaml and resolves the task state to operate
// on.
func loadStartContext(ticket string) (*config.ReposConfig, *taskstate.State, error) {
	rf, err := xdg.ReposFile()
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

	td, err := xdg.TasksDir()
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
	files, err := taskstate.ListFiles(tasksDirPath)
	if err != nil {
		return "", fmt.Errorf("list task files in %s: %w", tasksDirPath, err)
	}
	if len(files) <= 1 {
		return "", nil
	}
	if !cli.InteractiveAllowed() {
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
func startFlow(cfg *config.ReposConfig, ts *taskstate.State, opts startOpts) error {
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
			cli.Warnf("clearing stale active ticket record: %v", err)
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
	// A failed attempt is retried for the next target rather than skipped.
	recorded := false
	record := func() error {
		if recorded {
			return nil
		}
		if err := setActiveTicket(ts); err != nil {
			return fmt.Errorf("recording active ticket: %w", err)
		}
		recorded = true
		return nil
	}
	if len(panes) > 0 {
		if err := record(); err != nil {
			return err
		}
	}

	for _, pair := range selected {
		res := startOne(session, cfg.Tmux.ConfigFile, ts, pair, panes, opts.restart, cfg.HookTimeout())
		if res.launched() {
			if err := record(); err != nil {
				res.Status, res.Error = startStatusFailed, err.Error()
			}
		}
		if res.Status == startStatusFailed {
			cli.Warnf("failed to start %s: %s", res.Window, res.Error)
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
		if err := cli.PrintJSON(report); err != nil {
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
	if !cli.InteractiveAllowed() {
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

// startOne creates pair's window, respawns it if its pane is dead (or alive
// and restart is set), or leaves a live one alone. pre-run/post-run hooks run
// around every create or respawn (each killed after hookTimeout), never for a
// window left alone. A failing pre-run hook cancels this window: it is
// reported as failed and neither created nor respawned.
func startOne(session, configFile string, ts *taskstate.State, pair RunPair, panes []PaneInfo, restart bool, hookTimeout time.Duration) StartResult {
	res := StartResult{Target: pair.Label(), Window: pair.WindowName(), Status: startStatusStarted}
	pane, exists := findPane(panes, res.Window)
	if exists {
		res.Status = existingWindowStatus(pane, restart)
		if res.Status == startStatusAlreadyRunning {
			return res
		}
	}

	if err := runRunHooks(hooks.PreRun, ts, pair, hookTimeout); err != nil {
		res.Status, res.Error = startStatusFailed, fmt.Sprintf("not started: %v", err)
		return res
	}
	var err error
	if exists {
		err = respawnWindow(pane.WindowID, pair)
	} else {
		err = newWindow(session, configFile, res.Window, pair)
	}
	if err != nil {
		res.Status, res.Error = startStatusFailed, err.Error()
		return res
	}
	_ = runRunHooks(hooks.PostRun, ts, pair, hookTimeout) // post-run failures only warn
	return res
}
