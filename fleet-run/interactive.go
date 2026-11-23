package main

import (
	"os"

	"github.com/mattn/go-isatty"
)

// noInputEnv, when set to "1", disables every interactive picker even if
// stdin is a terminal (e.g. an agent running inside a terminal emulator).
const noInputEnv = "FLEET_NO_INPUT"

// interactiveAllowed reports whether a picker may be opened. Only stdin is
// checked, never stdout: commands are legitimately run under $(...) with
// stdout captured while a human still answers the picker. The pickers open
// /dev/tty themselves, so this must be checked BEFORE calling selectOne or
// selectMulti. isatty (not ModeCharDevice) is used because /dev/null is a
// character device too. A Cygwin/MSYS pty counts as a terminal, matching
// fleet-task's copy.
func interactiveAllowed() bool {
	fd := os.Stdin.Fd()
	return interactiveFrom(isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd), os.Getenv(noInputEnv))
}

// interactiveFrom is interactiveAllowed's pure decision, for testing.
func interactiveFrom(stdinIsTerminal bool, noInput string) bool {
	return stdinIsTerminal && noInput != "1"
}
