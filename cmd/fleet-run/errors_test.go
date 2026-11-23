package main

import (
	"fmt"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
)

func TestInputRequiredErrorIsUsage(t *testing.T) {
	if got := cli.ExitCode(fmt.Errorf("wrapped: %w", inputRequiredError("pass --all"))); got != cli.ExitUsage {
		t.Errorf("wrapped input-required -> %d", got)
	}
}
