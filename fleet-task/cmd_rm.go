package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdRm(ticket string) error {
	if ticket == "" {
		t, err := pickTicket()
		if err != nil {
			return err
		}
		ticket = t
	}

	// Catch the "still running under fleet-run" case up front with an
	// actionable error instead of racing it and hoping selfHealWorktreeRemove
	// covers whatever's left.
	if err := requireNotActive(ticket, "removing it"); err != nil {
		return err
	}

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
	// git run from the worktree itself) rather than aborting the whole rm.
	cfg := loadReposConfigOptional()

	var remaining []TaskRepo

	for _, r := range st.Repos {
		if rmErr := removeRepoWorktree(cfg, ticket, r); rmErr != nil {
			warnf("removing worktree %s: %v", r.WorktreePath, rmErr)
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

	// Every repo's worktree_path is <worktree_root>/<ticket>/<repo> (per
	// docs/CONTRACT.md, true for runtime: windows repos too -- that path is
	// the WSL-side symlink even though the real tree lives under
	// windows_worktree_root), so once every repo is torn down the
	// per-ticket parent directory is normally left empty and goes with it.
	// But it may also hold files the user keeps at the ticket root (notes,
	// env files; see `mv`, which moves them), so it's only removed if empty
	// -- never with RemoveAll.
	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		warnf("resolving worktree root to remove ticket workspace dir: %v", err)
		return nil
	}
	ticketDir := filepath.Join(wtRoot, ticket)
	removed, leftovers, err := removeDirIfEmpty(ticketDir)
	switch {
	case err != nil:
		warnf("removing ticket workspace dir %s: %v", ticketDir, err)
	case removed:
		fmt.Printf("removed ticket workspace dir %s\n", ticketDir)
	case len(leftovers) > 0:
		warnf("left ticket workspace dir %s in place; it still contains: %s", ticketDir, strings.Join(leftovers, ", "))
	}
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
		warnf("pre-remove hooks for %s: %v", r.Repo, err)
	}

	runner, base, err := gitForExisting(cfg, r.Repo, r.WorktreePath, "worktree remove")
	if err != nil {
		return err
	}
	removeErr := selfHealWorktreeRemove(r.WorktreePath, runner.WorktreeRemove(base, r.WorktreePath), func() error {
		return runner.Prune(base)
	})
	if removeErr != nil {
		return removeErr
	}

	if err := runHooks("post-remove", ticket, r.Repo, r.WorktreePath); err != nil {
		warnf("post-remove hooks for %s: %v", r.Repo, err)
	}
	return nil
}

// selfHealWorktreeRemove papers over a specific `git worktree remove --force`
// failure mode: git deregisters the worktree (removes its .git file/entry
// and everything it tracks) and only then hits a filesystem error trying to
// rmdir the now-empty-of-tracked-content directory, because something else
// -- typically a still-running dev server for this same ticket under
// `fleet-run`, writing into an untracked cache dir like `.vite/deps_temp_*`
// -- raced the delete and repopulated it, so git's own rmdir sees "Directory
// not empty". At that point git has fully committed to removing the
// worktree; there's nothing left to retry via git, so a plain rerun of `rm`
// just fails again with a *different*, more confusing error ("not a working
// tree") since git no longer considers the path a worktree at all. Detect
// that state (worktreePath still exists on disk but no longer has a .git
// entry inside it) and sweep the untracked leftovers ourselves instead of
// leaving the caller to retry something that can never succeed the same way
// twice.
//
// The other recoverable state is the mirror image: worktreePath is already
// gone (deleted by hand), so git's "remove" fails but there's nothing left
// to remove. That's treated as success after calling prune (which clears
// git's stale registration for it).
func selfHealWorktreeRemove(worktreePath string, removeErr error, prune func() error) error {
	if removeErr == nil {
		return nil
	}
	if _, statErr := os.Lstat(worktreePath); os.IsNotExist(statErr) {
		// The directory was already deleted by hand, so git can't remove
		// it -- but the goal (worktree gone) is met. Drop git's stale
		// registration (best-effort; a later `new` prunes again) and treat
		// the removal as done instead of failing forever.
		if pruneErr := prune(); pruneErr != nil {
			warnf("worktree prune after removing missing %s: %v", worktreePath, pruneErr)
		}
		fmt.Fprintf(os.Stderr, "note: %s was already gone from disk; pruned its worktree registration\n", worktreePath)
		return nil
	} else if statErr != nil {
		return removeErr
	}
	if !worktreeAdminGone(worktreePath) {
		// git still considers this a live worktree; the failure is real.
		return removeErr
	}
	if rmErr := os.RemoveAll(worktreePath); rmErr != nil {
		return fmt.Errorf("%w (also failed to remove leftover directory: %v)", removeErr, rmErr)
	}
	fmt.Fprintf(os.Stderr, "note: git had already deregistered %s; removed leftover directory\n", worktreePath)
	return nil
}

// worktreeAdminGone reports whether worktreePath no longer has a .git entry
// inside it, meaning git has already fully torn down its worktree
// registration and tracked content -- see selfHealWorktreeRemove.
func worktreeAdminGone(worktreePath string) bool {
	_, err := os.Lstat(filepath.Join(worktreePath, ".git"))
	return os.IsNotExist(err)
}
