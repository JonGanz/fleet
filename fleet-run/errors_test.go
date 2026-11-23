package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCodeFor(t *testing.T) {
	if got := exitCodeFor(errors.New("boom")); got != exitFailure {
		t.Errorf("plain error -> %d", got)
	}
	if got := exitCodeFor(usageErrorf("bad")); got != exitUsage {
		t.Errorf("usage error -> %d", got)
	}
	if got := exitCodeFor(fmt.Errorf("wrapped: %w", inputRequiredError("pass --all"))); got != exitUsage {
		t.Errorf("wrapped input-required -> %d", got)
	}
}
