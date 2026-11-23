package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// childStopGrace is how long a child (npm ci) gets to exit after SIGTERM
// before it is killed outright.
const childStopGrace = 10 * time.Second

// interruptContext returns a context cancelled on SIGINT or SIGTERM, so a
// cache build can stop npm and remove its staging dir instead of dying with
// it left behind. Only the first signal is caught: after it, the default
// behaviour is restored, so a second Ctrl-C still kills fleet-cache at once.
// Callers must call stop once the interruptible section is over.
func interruptContext() (ctx context.Context, stop context.CancelFunc) {
	ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

// stopOnCancel makes cmd (built with exec.CommandContext) receive SIGTERM
// rather than SIGKILL when its context is cancelled, so npm can stop its
// own children; it is killed if still running after childStopGrace.
func stopOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = childStopGrace
}
