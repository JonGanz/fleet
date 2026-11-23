package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fleet-run: "+err.Error())
		os.Exit(exitCodeFor(err))
	}
}

const (
	logsUsage = `Usage: fleet-run logs <repo:run-name> [-n|--lines <N>] [--json]

Print the last N lines (default 200) of a running window's output, including
scrollback. Works on dead panes, so a crashed app's output stays readable.

Flags:`
	startUsage = `Usage: fleet-run start [--ticket <id>] [--all] [repo:run-name ...] [--json]

Start windows for a ticket's apps: the named targets, every target (--all), or
a multiselect when neither is given (requires a terminal). Switches to the
ticket (stopping everything currently running) if it isn't already the active
one. A target whose window exists but whose app has exited is respawned.
Exits 1 if any selected target failed to start.

Flags:`
	statusUsage = `Usage: fleet-run status [--json]

Print the active ticket and the windows running in the fleet session, marking
windows whose app has exited. Exits 0 with "nothing running" if no tmux
server/session exists.

Flags:`
	stopUsage = `Usage: fleet-run stop [--ticket <id>] [--all] [repo:run-name ...] [--json]

Stop running windows: all of them (--all), the named repo:run-name targets, or
a multiselect over what's running (requires a terminal). Flags may appear
before or after the positional arguments.

Flags:`
	targetsUsage = `Usage: fleet-run targets [--ticket <id>] [--json]

List the repo:run-name targets available for a ticket, and whether each is
running.

Flags:`
)

const ticketFlagHelp = "ticket id (a picker opens if omitted and more than one task exists; without a terminal that is an error instead)"

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return usageErrorf("missing command")
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "logs":
		return runLogsCmd(rest)
	case "start":
		return runStartCmd(rest)
	case "status":
		return runStatusCmd(rest)
	case "stop":
		return runStopCmd(rest)
	case "targets":
		return runTargetsCmd(rest)
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		printUsage()
		return usageErrorf("unknown command %q", cmd)
	}
}

// ignoreHelp turns flag.ErrHelp (usage already printed) into a clean exit.
func ignoreHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `fleet-run - manage tmux windows for running fleet task apps

Only one ticket's windows are ever live at once. "start" for a different
ticket than whatever's currently active stops everything first; starting the
same ticket that's already active just adds any missing windows.

Usage:
  fleet-run start [--ticket <id>] [--all] [repo:run-name ...] [--json]
  fleet-run stop [--ticket <id>] [--all] [repo:run-name ...] [--json]
  fleet-run status [--json]
  fleet-run targets [--ticket <id>] [--json]
  fleet-run logs <repo:run-name> [-n|--lines <N>] [--json]

Without a terminal on stdin (or with FLEET_NO_INPUT=1) no picker is ever
opened: a command that would need one exits 2 naming the missing flag.
Exit codes: 0 ok, 1 operation failed, 2 usage error or input required.

Run 'fleet-run <command> --help' for a command's flags. See README.md for details.`)
}

func runLogsCmd(rest []string) error {
	fs := newFlagSet("logs")
	lines := 200
	fs.IntVar(&lines, "lines", 200, "number of lines to print, counted from the end")
	fs.IntVar(&lines, "n", 200, "shorthand for --lines")
	asJSON := fs.Bool("json", false, "print machine-readable JSON instead of text")
	pos, err := parseFlags(fs, logsUsage, rest)
	if err != nil {
		return ignoreHelp(err)
	}
	if len(pos) != 1 {
		return usageErrorf("logs takes exactly one repo:run-name argument (got %d)", len(pos))
	}
	return runLogs(pos[0], lines, *asJSON)
}

func runStartCmd(rest []string) error {
	fs := newFlagSet("start")
	var opts startOpts
	fs.StringVar(&opts.ticket, "ticket", "", ticketFlagHelp+"; switches to it, stopping whatever's currently running, if it's not already the active ticket")
	fs.BoolVar(&opts.all, "all", false, "start every target available for the ticket instead of multiselecting")
	fs.BoolVar(&opts.asJSON, "json", false, "print a machine-readable JSON result on stdout")
	pos, err := parseFlags(fs, startUsage, rest)
	if err != nil {
		return ignoreHelp(err)
	}
	opts.targets = pos
	return runStart(opts)
}

func runStatusCmd(rest []string) error {
	fs := newFlagSet("status")
	asJSON := fs.Bool("json", false, "print machine-readable JSON instead of text")
	pos, err := parseFlags(fs, statusUsage, rest)
	if err != nil {
		return ignoreHelp(err)
	}
	if len(pos) > 0 {
		return usageErrorf("status takes no positional arguments (got %q)", pos[0])
	}
	return runStatus(*asJSON)
}

func runStopCmd(rest []string) error {
	fs := newFlagSet("stop")
	var opts stopOpts
	fs.StringVar(&opts.ticket, "ticket", "", "assert this ticket is the currently active one; errors out (stopping nothing) if it isn't")
	fs.BoolVar(&opts.all, "all", false, "stop everything currently running instead of multiselecting")
	fs.BoolVar(&opts.asJSON, "json", false, "print a machine-readable JSON result on stdout")
	pos, err := parseFlags(fs, stopUsage, rest)
	if err != nil {
		return ignoreHelp(err)
	}
	opts.names = pos
	return runStop(opts)
}

func runTargetsCmd(rest []string) error {
	fs := newFlagSet("targets")
	ticket := fs.String("ticket", "", ticketFlagHelp)
	asJSON := fs.Bool("json", false, "print machine-readable JSON instead of a table")
	pos, err := parseFlags(fs, targetsUsage, rest)
	if err != nil {
		return ignoreHelp(err)
	}
	if len(pos) > 0 {
		return usageErrorf("targets takes no positional arguments (got %q)", pos[0])
	}
	return runTargets(*ticket, *asJSON)
}
