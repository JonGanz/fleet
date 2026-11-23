package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/JonGanz/fleet/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=<version>";
// see cli.Version for the fallback.
var version = "dev"

const (
	attachUsage = `Usage: fleet-run attach [repo:run-name]

Attach this terminal to the fleet tmux session (switch-client when already
inside a client of the same tmux server, else attach-session), first
selecting the named target's window if one is given. Needs a terminal on
stdin: without one, or with FLEET_NO_INPUT=1, exits 2. Exits 1 if nothing is
running or the named target has no window.

Flags:`
	logsUsage = `Usage: fleet-run logs <repo:run-name> [-n|--lines <N>] [--json]

Print the last N lines (default 200) of a running window's output, including
scrollback. Works on dead panes, so a crashed app's output stays readable.

Flags:`
	restartUsage = `Usage: fleet-run restart [--ticket <id>] [--all] [repo:run-name ...] [--json]

Like start (same target selection and ticket switching), but a target whose
app is still running is restarted (its pane respawned with the current
command) instead of left alone. Missing windows are created and dead panes
respawned, as with start. Exits 1 if any selected target failed.

Flags:`
	startUsage = `Usage: fleet-run start [--ticket <id>] [--all] [repo:run-name ...] [--json]

Start windows for a ticket's apps: the named targets, every target (--all), or
a multiselect when neither is given (requires a terminal). Switches to the
ticket (stopping everything currently running) if it isn't already the active
one. A target whose window exists but whose app has exited is respawned.
Executables in <config dir>/hooks/pre-run and post-run run around every
window created or respawned. Exits 1 if any selected target failed to start.

Flags:`
	statusUsage = `Usage: fleet-run status [--json]

Print the active ticket and the windows running in the fleet session, marking
windows whose app has exited, and flagging an active-ticket record with no
windows behind it as stale. Exits 0 with "nothing running" if no tmux
server/session exists.

Flags:`
	stopUsage = `Usage: fleet-run stop [--ticket <id>] [--all] [repo:run-name ...] [--json]

Stop running windows: all of them (--all), the named repo:run-name targets, or
a multiselect over what's running (requires a terminal). Flags may appear
before or after the positional arguments. Once nothing is left running the
active-ticket record is cleared -- also a stale one left behind when the
session was killed outside fleet-run.

Flags:`
	targetsUsage = `Usage: fleet-run targets [--ticket <id>] [--json]

List the repo:run-name targets available for a ticket, and whether each is
running.

Flags:`
)

const ticketFlagHelp = "ticket id (a picker opens if omitted and more than one task exists; without a terminal that is an error instead)"

// ignoreHelp turns flag.ErrHelp (usage already printed) into a clean exit.
func ignoreHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func main() {
	cli.SetProgram("fleet-run")
	os.Exit(cli.Report(run(os.Args[1:])))
}

// printUsage writes the top-level usage to w: stdout when help was asked
// for, stderr when shown because of a usage error.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, `fleet-run - manage tmux windows for running fleet task apps

Only one ticket's windows are ever live at once. "start" for a different
ticket than whatever's currently active stops everything first; starting the
same ticket that's already active just adds any missing windows.

Usage:
  fleet-run start [--ticket <id>] [--all] [repo:run-name ...] [--json]
  fleet-run restart [--ticket <id>] [--all] [repo:run-name ...] [--json]
  fleet-run stop [--ticket <id>] [--all] [repo:run-name ...] [--json]
  fleet-run status [--json]
  fleet-run targets [--ticket <id>] [--json]
  fleet-run logs <repo:run-name> [-n|--lines <N>] [--json]
  fleet-run attach [repo:run-name]
  fleet-run version

Without a terminal on stdin (or with FLEET_NO_INPUT=1) no picker is ever
opened: a command that would need one exits 2 naming the missing flag.
Exit codes: 0 ok, 1 operation failed, 2 usage error or input required.

Set FLEET_TMUX_SOCKET to use a private tmux server (tmux -L <name>).

Run 'fleet-run <command> --help' for a command's flags. See README.md for details.`)
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return cli.Usagef("missing command")
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "attach":
		return runAttachCmd(rest)
	case "logs":
		return runLogsCmd(rest)
	case "restart":
		return runRestartCmd(rest)
	case "start":
		return runStartCmd(rest)
	case "status":
		return runStatusCmd(rest)
	case "stop":
		return runStopCmd(rest)
	case "targets":
		return runTargetsCmd(rest)
	case "--version", "version":
		if len(rest) > 0 {
			return cli.Usagef("%s takes no arguments", cmd)
		}
		fmt.Println("fleet-run " + cli.Version(version))
		return nil
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return cli.Usagef("unknown command %q", cmd)
	}
}

func runAttachCmd(rest []string) error {
	fs := newFlagSet("attach")
	pos, err := parseFlags(fs, attachUsage, rest)
	if err != nil {
		return ignoreHelp(err)
	}
	if len(pos) > 1 {
		return cli.Usagef("attach takes at most one repo:run-name argument (got %d)", len(pos))
	}
	target := ""
	if len(pos) == 1 {
		target = pos[0]
	}
	return runAttach(target)
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
		return cli.Usagef("logs takes exactly one repo:run-name argument (got %d)", len(pos))
	}
	return runLogs(pos[0], lines, *asJSON)
}

func runRestartCmd(rest []string) error {
	return runStartLike("restart", restartUsage, true, rest)
}

func runStartCmd(rest []string) error {
	return runStartLike("start", startUsage, false, rest)
}

// runStartLike parses start's flags, shared by `start` and `restart`.
func runStartLike(name, usage string, restart bool, rest []string) error {
	fs := newFlagSet(name)
	opts := startOpts{restart: restart}
	fs.StringVar(&opts.ticket, "ticket", "", ticketFlagHelp+"; switches to it, stopping whatever's currently running, if it's not already the active ticket")
	fs.BoolVar(&opts.all, "all", false, name+" every target available for the ticket instead of multiselecting")
	fs.BoolVar(&opts.asJSON, "json", false, "print a machine-readable JSON result on stdout")
	pos, err := parseFlags(fs, usage, rest)
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
		return cli.Usagef("status takes no positional arguments (got %q)", pos[0])
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
		return cli.Usagef("targets takes no positional arguments (got %q)", pos[0])
	}
	return runTargets(*ticket, *asJSON)
}
