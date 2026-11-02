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
		os.Exit(1)
	}
}

const (
	startUsage = `Usage: fleet-run start [--ticket <id>]

Start windows for a ticket's apps. Switches to the ticket (stopping everything
currently running) if it isn't already the active one. Exits 1 if any selected
target failed to start.

Flags:`
	stopUsage = `Usage: fleet-run stop [--ticket <id>] [--all] [repo:run-name ...]

Stop running windows: all of them (--all), the named repo:run-name targets, or
a multiselect over what's running. Flags may appear before or after the
positional arguments.

Flags:`
	statusUsage = `Usage: fleet-run status [--json]

Print the active ticket and the windows running in the fleet session. Exits 0
with "nothing running" if no tmux server/session exists.

Flags:`
)

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("missing command")
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "start":
		fs := newFlagSet("start")
		ticket := fs.String("ticket", "", "ticket id to start windows for (a picker opens if omitted and more than one task exists); switches to it, stopping whatever's currently running, if it's not already the active ticket")
		pos, err := parseFlags(fs, startUsage, rest)
		if err != nil {
			return ignoreHelp(err)
		}
		if len(pos) > 0 {
			return fmt.Errorf("start takes no positional arguments (got %q); did you mean --ticket?", pos[0])
		}
		return runStart(*ticket)

	case "stop":
		fs := newFlagSet("stop")
		ticket := fs.String("ticket", "", "assert this ticket is the currently active one; errors out (stopping nothing) if it isn't")
		all := fs.Bool("all", false, "stop everything currently running instead of multiselecting")
		pos, err := parseFlags(fs, stopUsage, rest)
		if err != nil {
			return ignoreHelp(err)
		}
		return runStop(*ticket, *all, pos)

	case "status":
		fs := newFlagSet("status")
		asJSON := fs.Bool("json", false, "print machine-readable JSON instead of text")
		pos, err := parseFlags(fs, statusUsage, rest)
		if err != nil {
			return ignoreHelp(err)
		}
		if len(pos) > 0 {
			return fmt.Errorf("status takes no positional arguments (got %q)", pos[0])
		}
		return runStatus(*asJSON)

	case "-h", "--help", "help":
		printUsage()
		return nil

	default:
		printUsage()
		return fmt.Errorf("unknown command %q", cmd)
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
  fleet-run start [--ticket <id>]
  fleet-run stop [--ticket <id>] [--all] [repo:run-name ...]
  fleet-run status [--json]

Run 'fleet-run <command> --help' for a command's flags. See README.md for details.`)
}
