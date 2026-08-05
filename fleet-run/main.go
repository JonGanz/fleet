package main

import (
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

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("missing command")
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "start":
		fs := flag.NewFlagSet("start", flag.ContinueOnError)
		ticket := fs.String("ticket", "", "ticket id to start windows for (required if more than one task exists); switches to it, stopping whatever's currently running, if it's not already the active ticket")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return runStart(*ticket)

	case "stop":
		fs := flag.NewFlagSet("stop", flag.ContinueOnError)
		ticket := fs.String("ticket", "", "assert this ticket is the currently active one; errors out (stopping nothing) if it isn't")
		all := fs.Bool("all", false, "stop everything currently running instead of multiselecting")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return runStop(*ticket, *all, fs.Args())

	case "-h", "--help", "help":
		printUsage()
		return nil

	default:
		printUsage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `fleet-run - manage tmux windows for running fleet task apps

Only one ticket's windows are ever live at once. "start" for a different
ticket than whatever's currently active stops everything first; starting the
same ticket that's already active just adds any missing windows.

Usage:
  fleet-run start [--ticket <id>]
  fleet-run stop [--ticket <id>] [--all] [repo:run-name ...]

See README.md for details.`)
}
