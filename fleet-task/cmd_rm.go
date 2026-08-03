package main

import (
	"fmt"
	"os"
)

func cmdRm(ticket string) error {
	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	st, err := readTaskState(tf)
	if err != nil {
		return fmt.Errorf("read task %s: %w", ticket, err)
	}

	// repos.yaml is optional here: if it's missing or no longer has the
	// repo entry, we still try to remove the worktree (best-effort, via
	// git run from the worktree's parent) rather than aborting the whole
	// rm.
	var cfg *ReposConfig
	if rf, err := reposFile(); err == nil {
		if c, err := loadReposConfig(rf); err == nil {
			cfg = c
		}
	}

	for _, r := range st.Repos {
		repo := cfg.findRepo(r.Repo)
		var base string
		if repo != nil {
			base = expandHome(repo.Base)
		}

		var rmErr error
		if base != "" {
			rmErr = gitWorktreeRemove(base, r.WorktreePath)
		} else {
			fmt.Fprintf(os.Stderr, "warning: repo %q not found in repos.yaml, trying worktree remove without a base\n", r.Repo)
			rmErr = runGit("-C", r.WorktreePath, "worktree", "remove", r.WorktreePath, "--force")
		}

		if rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: removing worktree %s: %v\n", r.WorktreePath, rmErr)
			continue
		}
		fmt.Printf("removed worktree %s (%s)\n", r.WorktreePath, r.Repo)
	}

	if err := os.Remove(tf); err != nil {
		return fmt.Errorf("remove task state %s: %w", tf, err)
	}
	fmt.Printf("removed task state %s\n", tf)
	return nil
}
