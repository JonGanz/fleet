package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// Hook phases fleet-run runs, under <config dir>/hooks/<phase>/.
const (
	hookPhasePostRun = "post-run"
	hookPhasePreRun  = "pre-run"
)

// discoverHooks returns the executable regular files directly in dir, in
// filename-sorted order. Non-executable files and directories are skipped; a
// missing dir means no hooks. Mirrors fleet-task's discoverHooks (no shared
// code between the modules by design).
func discoverHooks(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var hooks []string
	for _, name := range names {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p) // follows symlinks, so a linked script counts
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		hooks = append(hooks, p)
	}
	return hooks, nil
}

// hookEnv is the environment added for a run hook, on top of fleet-run's own.
func hookEnv(ts *TaskState, p RunPair) []string {
	return []string{
		"FLEET_TICKET=" + ts.Ticket,
		"FLEET_REPO=" + p.Repo,
		"FLEET_WORKTREE_DIR=" + p.WorktreeDir,
		"FLEET_BRANCH=" + p.Branch,
		"FLEET_DESCRIPTION=" + ts.Description,
		"FLEET_RUN_NAME=" + p.RunName,
	}
}

// hooksDir returns <config dir>/hooks/<phase>.
func hooksDir(phase string) (string, error) {
	cd, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "hooks", phase), nil
}

// runRunHooks runs the phase's hooks for one target's window. A hook's stdout
// and stderr both go to fleet-run's stderr (stdout is reserved for results).
// Failures only warn: like fleet-task's hooks, they never block the launch.
func runRunHooks(phase string, ts *TaskState, p RunPair) {
	dir, err := hooksDir(phase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s hooks: %v\n", phase, err)
		return
	}
	hooks, err := discoverHooks(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s hooks: %v\n", phase, err)
		return
	}
	env := append(os.Environ(), hookEnv(ts, p)...)
	for _, h := range hooks {
		cmd := exec.Command(h)
		cmd.Env = env
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s hook %s failed for %s: %v\n", phase, h, p.Label(), err)
		}
	}
}
