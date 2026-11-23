package main

import (
	"os"

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
// target's window. A hook's stdout and stderr both go to fleet-run's stderr
// (stdout is reserved for results). Failures only warn: like fleet-task's
// hooks, they never block the launch.
func runRunHooks(phase string, ts *taskstate.State, p RunPair) {
	if err := hooks.Run(phase, hookEnv(ts, p), os.Stderr); err != nil {
		cli.Warnf("%s hooks: %v", phase, err)
	}
}
