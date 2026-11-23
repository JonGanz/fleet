package main

import "github.com/JonGanz/fleet/internal/cli"

// inputRequiredError is returned instead of opening a picker when no
// terminal is available: a usage error (exit 2). hint names the flag or
// argument that avoids the picker.
func inputRequiredError(hint string) error {
	return cli.Usagef("input required but no terminal available (or FLEET_NO_INPUT=1): %s", hint)
}
