package main

import (
	"fmt"
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

// runHooks runs each hook found in <config dir>/hooks/<phase>/* in
// filename-sorted order, with FLEET_TICKET/FLEET_REPO/FLEET_WORKTREE_DIR
// set in addition to the inherited environment. A non-zero exit from a
// hook prints a warning to stderr but does not abort the caller.
func runHooks(phase, ticket, repo, worktreeDir string) error {
	dir, err := hooksDir(phase)
	if err != nil {
		return err
	}
	hooks, err := discoverHooks(dir)
	if err != nil {
		return err
	}
	env := append(os.Environ(),
		"FLEET_TICKET="+ticket,
		"FLEET_REPO="+repo,
		"FLEET_WORKTREE_DIR="+worktreeDir,
	)
	for _, h := range hooks {
		cmd := exec.Command(h)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hook %s failed: %v\n", h, err)
		}
	}
	return nil
}
