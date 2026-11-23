package main

import (
	"os"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/hooks"
	"github.com/JonGanz/fleet/internal/taskstate"
)

// hookEnv is what a run hook is told about the target it runs for.
func hookEnv(ts *taskstate.State, p RunPair) hooks.Env {
	return hooks.Env{
		Ticket:      ts.Ticket,
		Description: ts.Description,
		Repo:        p.Repo,
		Branch:      p.Branch,
		WorktreeDir: p.WorktreeDir,
		RunName:     p.RunName,
	}
}

// runRunHooks runs the phase's hooks (hooks.PreRun or hooks.PostRun) for one
// target's window, each killed after timeout. A hook's stdout and stderr both
// go to fleet-run's stderr (stdout is reserved for results). A failing
// pre-run hook vetoes the window: the error is returned and the window must
// not be launched. A failing post-run hook only warns (hooks.Run does), and
// a failure to find post-run hooks is warned about here; nil is returned.
func runRunHooks(phase string, ts *taskstate.State, p RunPair, timeout time.Duration) error {
	err := hooks.Run(phase, hookEnv(ts, p), os.Stderr, timeout)
	if err == nil || hooks.Vetoes(phase) {
		return err
	}
	cli.Warnf("%s hooks: %v", phase, err)
	return nil
}
