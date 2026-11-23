package main

import (
	"errors"
	"fmt"
)

// Exit codes. Usage errors (bad flags/arguments) and "input required" errors
// (a picker would have been needed but no terminal is available) share code
// 2, so scripts and agents can tell "you called me wrong" apart from "the
// operation itself failed" (code 1).
const (
	exitFailure = 1
	exitUsage   = 2
)

// usageError marks an error as a usage / input-required error (exit code 2).
type usageError struct {
	msg string
}

func (e *usageError) Error() string {
	return e.msg
}

// exitCodeFor maps an error returned by run() to the process exit code.
func exitCodeFor(err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		return exitUsage
	}
	return exitFailure
}

// inputRequiredError is returned instead of opening a picker when no
// terminal is available. hint names the flag or argument that avoids the
// picker.
func inputRequiredError(hint string) error {
	return &usageError{msg: "input required but no terminal available (or FLEET_NO_INPUT=1): " + hint}
}

// usageErrorf builds a usage error (exit code 2).
func usageErrorf(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}
