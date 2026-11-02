package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// cmdSpec describes one subcommand for parseCmdArgs: its usage synopsis
// (shown by --help and in parse errors) and how many positional arguments it
// accepts.
type cmdSpec struct {
	name     string
	synopsis string // e.g. "rm [ticket]"
	summary  string
	maxPos   int
}

// parseCmdArgs parses args for one subcommand with its own flag.FlagSet.
// Unlike the stdlib's default, flags and positionals may be interspersed
// (`rm PROJ-1 --json` and `rm --json PROJ-1` behave the same), and a bare
// `--` ends flag parsing. setup registers the subcommand's flags. Returns the
// positional arguments; flag.ErrHelp if -h/--help was given (usage has
// already been printed to stdout, callers should exit 0).
func parseCmdArgs(spec cmdSpec, args []string, setup func(fs *flag.FlagSet)) ([]string, error) {
	fs := flag.NewFlagSet(spec.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	if setup != nil {
		setup(fs)
	}

	var pos []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				printCmdUsage(os.Stdout, spec, fs)
				return nil, flag.ErrHelp
			}
			return nil, fmt.Errorf("%w (see `fleet-task %s --help`)", err, spec.name)
		}
		remaining := fs.Args()
		if len(remaining) == 0 {
			break
		}
		consumed := len(rest) - len(remaining)
		if consumed > 0 && rest[consumed-1] == "--" {
			pos = append(pos, remaining...)
			break
		}
		pos = append(pos, remaining[0])
		rest = remaining[1:]
	}

	if len(pos) > spec.maxPos {
		return nil, fmt.Errorf("too many arguments: usage is `fleet-task %s` (see `fleet-task %s --help`)", spec.synopsis, spec.name)
	}
	return pos, nil
}

func printCmdUsage(w io.Writer, spec cmdSpec, fs *flag.FlagSet) {
	fmt.Fprintf(w, "usage: fleet-task %s\n\n%s\n", spec.synopsis, spec.summary)
	hasFlags := false
	fs.VisitAll(func(*flag.Flag) { hasFlags = true })
	if hasFlags {
		fmt.Fprintln(w, "\nflags:")
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
}
