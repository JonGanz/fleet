package main

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestStopOnCancelStopsChild(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sleep", "30")
	stopOnCancel(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Error("a cancelled child should report an error")
	}
	if time.Since(start) > childStopGrace/2 {
		t.Errorf("child took %v to stop; SIGTERM should end it promptly", time.Since(start))
	}
}
