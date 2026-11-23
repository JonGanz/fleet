package main

import (
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
)

// The version fallback rules are tested in internal/cli; this checks that
// fleet-run reports its -ldflags stamp.
func TestVersionStamped(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "1.2.3"
	if got := cli.Version(version); got != "1.2.3" {
		t.Errorf("got %q", got)
	}
}
