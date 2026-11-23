// Package proc runs child processes with a time limit, shared by fleet-task
// (network git, hooks) and fleet-run (hooks). On unix a child can be put in
// its own process group, so a timeout kills everything it started (a hook's
// background jobs, git's ssh) instead of leaving them running.
package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// waitDelay bounds how long Wait keeps waiting for a killed (or exited)
// child's output pipes to close: a grandchild that escaped the process group
// (setsid) can hold them open forever.
const waitDelay = 5 * time.Second

// Options says how Run treats one child process.
type Options struct {
	// Timeout kills the child once it has run this long; 0 means no limit.
	Timeout time.Duration
	// NewGroup runs the child in its own process group (unix only; ignored
	// elsewhere), so the timeout kills the whole group. While it runs,
	// SIGINT/SIGTERM/SIGHUP sent to this process are passed on to the group
	// and then re-raised here, so Ctrl-C still stops both as before. A child
	// in its own group is not in the terminal's foreground group, so it must
	// not need to read from the terminal (it would be stopped by SIGTTIN).
	NewGroup bool
}

// TimeoutError is returned by Run when the child was killed by the timeout.
type TimeoutError struct {
	Timeout time.Duration
	// Err is what Wait reported for the killed child.
	Err error
}

// IsTimeout reports whether err is (or wraps) a *TimeoutError.
func IsTimeout(err error) bool {
	var te *TimeoutError
	return errors.As(err, &te)
}

// Run starts cmd and waits for it, applying opts. cmd must not have been
// started. The error is cmd.Start's or cmd.Wait's, or a *TimeoutError
// (wrapping Wait's) if the timeout killed it.
func Run(cmd *exec.Cmd, opts Options) error {
	if opts.NewGroup {
		setNewGroup(cmd)
	}
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = waitDelay
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if opts.NewGroup {
		defer forwardSignals(cmd.Process.Pid)()
	}

	// done and timedOut are guarded by mu so the timer never kills a
	// process (group) that Wait has already reaped and whose id the system
	// may hand out again.
	var (
		mu       sync.Mutex
		done     bool
		timedOut bool
	)
	if opts.Timeout > 0 {
		timer := time.AfterFunc(opts.Timeout, func() {
			mu.Lock()
			defer mu.Unlock()
			if done {
				return
			}
			timedOut = true
			kill(cmd.Process, opts.NewGroup)
		})
		defer timer.Stop()
	}

	err := cmd.Wait()
	mu.Lock()
	done = true
	killed := timedOut
	mu.Unlock()
	if killed {
		return &TimeoutError{Timeout: opts.Timeout, Err: err}
	}
	return err
}

// Error says the child was killed and after how long.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("killed after the %s timeout", e.Timeout)
}

// Unwrap returns Wait's error.
func (e *TimeoutError) Unwrap() error { return e.Err }
