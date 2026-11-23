package main

import (
	"io"
	"os"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
)

// out receives human-readable progress output. It is os.Stdout normally and
// os.Stderr under --json, so stdout carries nothing but the JSON document.
var out io.Writer = os.Stdout

// listFlag is a repeatable flag.Value that also accepts comma-separated
// values: `--repo a --repo b` and `--repo a,b` are equivalent. Blank
// entries are dropped.
type listFlag []string

// trackedString is a string flag.Value that remembers whether it was set,
// so an explicitly empty value (`-d ""`) is distinguishable from absence.
type trackedString struct {
	value string
	set   bool
}

func (l *listFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

func (l *listFlag) String() string { return strings.Join(*l, ",") }

// needInput returns the error for a value that would have to be prompted for
// when prompting isn't allowed. hint names the flag/argument that supplies it.
func needInput(what, hint string) error {
	return cli.Usagef("missing %s (no terminal is available for a prompt); %s", what, hint)
}

func (t *trackedString) Set(v string) error {
	t.value, t.set = v, true
	return nil
}

func (t *trackedString) String() string { return t.value }

// useJSONOutput routes human-readable output to stderr when jsonOut is set.
func useJSONOutput(jsonOut bool) {
	if jsonOut {
		out = os.Stderr
	}
}
