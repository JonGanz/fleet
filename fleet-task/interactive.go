package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// out receives human-readable progress output. It is os.Stdout normally and
// os.Stderr under --json, so stdout carries nothing but the JSON document.
var out io.Writer = os.Stdout

// usageError marks a failure caused by how the command was invoked (bad
// flags, unknown repo/patch names, or input that would need an interactive
// prompt in a non-interactive context). main exits 2 for it instead of 1.
type usageError struct {
	msg string
}

func (e *usageError) Error() string { return e.msg }

// listFlag is a repeatable flag.Value that also accepts comma-separated
// values: `--repo a --repo b` and `--repo a,b` are equivalent. Blank
// entries are dropped.
type listFlag []string

func (l *listFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

func (l *listFlag) String() string { return strings.Join(*l, ",") }

// trackedString is a string flag.Value that remembers whether it was set,
// so an explicitly empty value (`-d ""`) is distinguishable from absence.
type trackedString struct {
	value string
	set   bool
}

func (t *trackedString) Set(v string) error {
	t.value, t.set = v, true
	return nil
}

func (t *trackedString) String() string { return t.value }

// interactiveAllowed reports whether pickers and prompts may be shown:
// stdin must be a real terminal (not a pipe or /dev/null) and FLEET_NO_INPUT
// must not be "1". Only stdin is checked -- stdout is legitimately captured
// by `cd "$(fleet-task jump)"`. The pickers render on /dev/tty, so without
// this guard an agent could open a picker on its user's terminal and hang.
func interactiveAllowed() bool {
	if os.Getenv("FLEET_NO_INPUT") == "1" {
		return false
	}
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// isUsageError reports whether err is (or wraps) a usageError.
func isUsageError(err error) bool {
	var ue *usageError
	return errors.As(err, &ue)
}

// needInput returns the error for a value that would have to be prompted for
// when prompting isn't allowed. hint names the flag/argument that supplies it.
func needInput(what, hint string) error {
	return &usageError{msg: fmt.Sprintf("missing %s (no terminal is available for a prompt); %s", what, hint)}
}

// usageErrorf builds a usageError from a format string.
func usageErrorf(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// useJSONOutput routes human-readable output to stderr when jsonOut is set.
func useJSONOutput(jsonOut bool) {
	if jsonOut {
		out = os.Stderr
	}
}
