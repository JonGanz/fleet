package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// newFlagSet builds a quiet per-subcommand FlagSet: flag's own error/usage
// printing is suppressed (parseFlags reports errors, run() prints them once).
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses args against fs allowing flags both before and after
// positional arguments (Go's flag package stops at the first positional), and
// returns the positionals. A literal "--" ends flag parsing; everything after
// it is positional. On -h/--help it prints usage (header plus flag defaults)
// to stderr and returns flag.ErrHelp.
func parseFlags(fs *flag.FlagSet, usage string, args []string) ([]string, error) {
	var positional []string
	for {
		err := fs.Parse(args)
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, usage)
			fs.SetOutput(os.Stderr)
			fs.PrintDefaults()
			fs.SetOutput(io.Discard)
			return nil, flag.ErrHelp
		}
		if err != nil {
			return nil, fmt.Errorf("%w (see 'fleet-run %s --help')", err, fs.Name())
		}
		rest := fs.Args()
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
