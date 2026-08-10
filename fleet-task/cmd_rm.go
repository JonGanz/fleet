package main

import (
	"fmt"
	"os"
	"path/filepath"
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

	var remaining []TaskRepo

	for _, r := range st.Repos {
		if rmErr := removeRepoWorktree(cfg, ticket, r); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: removing worktree %s: %v\n", r.WorktreePath, rmErr)
			remaining = append(remaining, r)
			continue
		}
		fmt.Printf("removed worktree %s (%s)\n", r.WorktreePath, r.Repo)
	}

	// If any worktree removal failed, keep the task state around (with only
	// the still-failing repos listed) instead of deleting it, so a rerun of
	// `rm` retries just those and the ticket doesn't silently vanish from
	// `fleet-task list`/`fleet-run` while its worktrees still exist on disk.
	if len(remaining) > 0 {
		st.Repos = remaining
		if err := writeTaskState(tf, st); err != nil {
			return fmt.Errorf("update task state %s after partial removal: %w", tf, err)
		}
		return fmt.Errorf("%d repo(s) failed to remove for %s; task state kept at %s for retry", len(remaining), ticket, tf)
	}

	if err := os.Remove(tf); err != nil {
		return fmt.Errorf("remove task state %s: %w", tf, err)
	}
	fmt.Printf("removed task state %s\n", tf)
	return nil
}

// removeRepoWorktree tears down a single repo's worktree, shared between
// `rm` (removing every repo in a task) and `edit`'s remove-repo path
// (removing a selected subset). cfg may be nil, or missing this repo's
// entry entirely (repos.yaml changed, or the repo was dropped from it,
// since the task was created) -- best-effort falls back to a bare
// `git worktree remove` run against the worktree itself rather than
// aborting. pre-remove/post-remove hooks run around the teardown itself;
// like pre-create/post-create, hook failures are non-fatal (warned to
// stderr by runHooks) and never block the removal.
func removeRepoWorktree(cfg *ReposConfig, ticket string, r TaskRepo) error {
	if err := runHooks("pre-remove", ticket, r.Repo, r.WorktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: pre-remove hooks for %s: %v\n", r.Repo, err)
	}

	repo := cfg.findRepo(r.Repo)

	var removeErr error
	switch {
	case repo != nil && repo.Runtime == "windows":
		removeErr = removeWorktreeWindows(repo, r.WorktreePath)
	case repo != nil && expandHome(repo.Base) != "":
		removeErr = gitWorktreeRemove(expandHome(repo.Base), r.WorktreePath)
	default:
		fmt.Fprintf(os.Stderr, "warning: repo %q not found in repos.yaml, trying worktree remove without a base\n", r.Repo)
		removeErr = runGit("-C", r.WorktreePath, "worktree", "remove", r.WorktreePath, "--force")
	}
	if removeErr != nil {
		return removeErr
	}

	if err := runHooks("post-remove", ticket, r.Repo, r.WorktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: post-remove hooks for %s: %v\n", r.Repo, err)
	}
	return nil
}

// removeWorktreeWindows tears down a runtime: windows repo's worktree: the
// real git worktree (resolved via the WSL-side symlink) is deregistered
// through Windows-native git.exe, then the symlink itself is removed. If we
// only removed the symlink, the real Windows-native tree and its worktree
// registration in windows_base would silently leak.
func removeWorktreeWindows(repo *RepoConfig, worktreePath string) error {
	if repo.WindowsBase == "" {
		return fmt.Errorf("repo %s is runtime: windows but has no windows_base", repo.Name)
	}
	winBaseWin, err := wslToWindowsPath(expandHome(repo.WindowsBase))
	if err != nil {
		return fmt.Errorf("resolving windows_base: %w", err)
	}

	realWorktree, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		// Symlink is already gone/broken; nothing left to deregister on the
		// Windows side, just fall through to removing whatever's here.
		fmt.Fprintf(os.Stderr, "warning: resolving symlink %s: %v\n", worktreePath, err)
	} else {
		winWorktreeWin, err := wslToWindowsPath(realWorktree)
		if err != nil {
			return fmt.Errorf("resolving worktree path: %w", err)
		}
		if err := winGitWorktreeRemove(winBaseWin, winWorktreeWin); err != nil {
			return fmt.Errorf("git worktree remove: %w", err)
		}
	}

	if err := os.Remove(worktreePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing symlink %s: %w", worktreePath, err)
	}
	return nil
}
