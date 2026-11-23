package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// discoverHooks globs <dir>/* and returns the paths of executable regular
// files, in filename-sorted order. Non-executable files (and directories)
// are silently skipped. If dir does not exist, returns an empty slice and
// no error.
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
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		hooks = append(hooks, p)
	}
	return hooks, nil
}

// hookContext is what a create/remove hook is told about the repo it runs
// for, exported to it as FLEET_* environment variables (see hookEnv).
type hookContext struct {
	Ticket      string
	Description string
	Repo        string
	Branch      string
	WorktreeDir string
}

// hookEnv returns base plus the FLEET_* variables for h.
func hookEnv(base []string, h hookContext) []string {
	env := append([]string{}, base...)
	return append(env,
		"FLEET_TICKET="+h.Ticket,
		"FLEET_DESCRIPTION="+h.Description,
		"FLEET_REPO="+h.Repo,
		"FLEET_BRANCH="+h.Branch,
		"FLEET_WORKTREE_DIR="+h.WorktreeDir,
	)
}

// runHooks runs each hook found in <config dir>/hooks/<phase>/* in
// filename-sorted order, with the hookEnv variables set in addition to the
// inherited environment. A non-zero exit from a hook prints a warning to
// stderr but does not abort the caller.
func runHooks(phase string, h hookContext) error {
	dir, err := hooksDir(phase)
	if err != nil {
		return err
	}
	hooks, err := discoverHooks(dir)
	if err != nil {
		return err
	}
	env := hookEnv(os.Environ(), h)
	for _, path := range hooks {
		cmd := exec.Command(path)
		cmd.Env = env
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			warnf("hook %s failed: %v", path, err)
		}
	}
	return nil
}
