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
		ticket := fs.String("ticket", "", "ticket id to start windows for (required if more than one task exists)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return runStart(*ticket)

	case "stop":
		fs := flag.NewFlagSet("stop", flag.ContinueOnError)
		ticket := fs.String("ticket", "", "ticket id to scope the stop to")
		all := fs.Bool("all", false, "stop all matching windows instead of multiselecting")
		everything := fs.Bool("everything", false, "confirm stopping all windows across all tickets when --all is used without --ticket")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return runStop(*ticket, *all, *everything, fs.Args())

	case "switch":
		fs := flag.NewFlagSet("switch", flag.ContinueOnError)
		to := fs.String("to", "", "ticket id to switch to (required)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return runSwitch(*to)

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

Usage:
  fleet-run start [--ticket <id>]
  fleet-run stop [--ticket <id>] [--all [--everything]] [repo:run-name ...]
  fleet-run switch --to <ticket>

See README.md for details.`)
}
