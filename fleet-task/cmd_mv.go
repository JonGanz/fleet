package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdMv renames an existing task's ticket id: the state file (and its
// `Ticket` field) and every attached repo's worktree directory move from
// old to new. Branches are left untouched -- a repo's branch name comes
// from a template (branchName) that isn't necessarily equal to the ticket
// string, and renaming a possibly-already-pushed branch is a separate,
// riskier operation than renaming the ticket identity itself.
//
// mv is resumable: after each repo's worktree moves, the old ticket's state
// file is rewritten with that repo's new worktree path, so state always
// reflects reality. If a later repo fails, rerunning `mv old new` skips the
// repos already at their destination and carries on with the rest.
func cmdMv(oldTicket, newTicket string) error {
	if oldTicket == "" {
		t, err := pickTicket()
		if err != nil {
			return err
		}
		oldTicket = t
	}
	if err := validateTicketName(oldTicket); err != nil {
		return err
	}

	if err := requireNotActive(oldTicket, "renaming it"); err != nil {
		return err
	}

	oldFile, err := taskFile(oldTicket)
	if err != nil {
		return err
	}
	st, err := readTaskState(oldFile)
	if err != nil {
		return fmt.Errorf("read task %s: %w", oldTicket, err)
	}

	if newTicket == "" {
		values, err := promptForm([]string{"New ticket"})
		if err != nil {
			return fmt.Errorf("read new ticket: %w", err)
		}
		newTicket = values[0]
	}
	if err := validateTicketName(newTicket); err != nil {
		return err
	}
	if newTicket == oldTicket {
		return fmt.Errorf("new ticket %q is the same as the old ticket", newTicket)
	}

	newFile, err := taskFile(newTicket)
	if err != nil {
		return err
	}
	if _, err := os.Stat(newFile); err == nil {
		return fmt.Errorf("a task already exists at %s", newFile)
	}

	cfg := loadReposConfigOptional()

	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		return err
	}
	oldTicketDir := filepath.Join(wtRoot, oldTicket)
	newTicketDir := filepath.Join(wtRoot, newTicket)
	if err := checkNewTicketDir(newTicketDir, st.Repos); err != nil {
		return err
	}

	// Move one repo at a time, persisting after each. A half-moved task is
	// recorded as exactly that (moved repos carry their new paths), so the
	// state file never points at directories that no longer exist, and a
	// rerun resumes where this one stopped.
	for i := range st.Repos {
		r := st.Repos[i]
		newWorktreePath := filepath.Join(newTicketDir, r.Repo)

		if alreadyMoved(r.WorktreePath, newWorktreePath) {
			fmt.Printf("worktree %s already at %s, skipping\n", r.Repo, newWorktreePath)
			st.Repos[i].WorktreePath = newWorktreePath
			continue
		}

		if err := moveRepoWorktree(cfg, r, newWorktreePath); err != nil {
			remaining := st.Repos[i:]
			// Moves are persisted as they happen; this only flushes paths
			// updated by the "already moved" skip branch above.
			if werr := writeTaskState(oldFile, st); werr != nil {
				warnf("update task state %s: %v", oldFile, werr)
			}
			return fmt.Errorf(
				"moving worktree %s (%s): %w\n%d of %d repo(s) moved to %s; %d (%s) not yet -- fix the problem and rerun `fleet-task mv %s %s` to finish (state still lives under %s)",
				r.WorktreePath, r.Repo, err, i, len(st.Repos), newTicketDir, len(remaining), repoNames(remaining), oldTicket, newTicket, oldTicket,
			)
		}
		fmt.Printf("moved worktree %s -> %s (%s)\n", r.WorktreePath, newWorktreePath, r.Repo)
		st.Repos[i].WorktreePath = newWorktreePath
		if err := writeTaskState(oldFile, st); err != nil {
			return fmt.Errorf("update task state %s after moving %s (worktree is already at %s): %w", oldFile, r.Repo, newWorktreePath, err)
		}
	}

	st.Ticket = newTicket
	if err := writeTaskState(newFile, st); err != nil {
		return fmt.Errorf("write task state %s: %w", newFile, err)
	}
	if err := os.Remove(oldFile); err != nil {
		return fmt.Errorf("remove old task state %s (new state already written to %s): %w", oldFile, newFile, err)
	}
	os.Remove(oldFile + ".lock") // best-effort: stale lockfile from a crashed prior run, if any

	if err := moveTicketDirLeftovers(oldTicketDir, newTicketDir); err != nil {
		warnf("%v", err)
	}

	fmt.Printf("renamed task %s -> %s; state written to %s\n", oldTicket, newTicket, newFile)
	return nil
}

// alreadyMoved reports whether a repo's worktree is already at its
// destination: the state path equals it (a prior `mv` run moved it and
// recorded that), or the source is gone and the destination exists (a
// prior run was interrupted between the move and the state write).
func alreadyMoved(oldPath, newPath string) bool {
	if oldPath == newPath {
		return true
	}
	if _, err := os.Lstat(oldPath); !os.IsNotExist(err) {
		return false
	}
	_, err := os.Lstat(newPath)
	return err == nil
}

// checkNewTicketDir decides whether newDir is an acceptable destination for
// `mv`. A missing or empty dir is fine, and so is one containing only
// worktrees of this very task that an earlier, interrupted `mv` already
// moved there (alreadyMoved). Anything else -- another task's worktrees,
// unrelated files, a repo dir that isn't ours -- is refused rather than
// merged into.
func checkNewTicketDir(newDir string, repos []TaskRepo) error {
	entries, err := os.ReadDir(newDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", newDir, err)
	}
	byName := make(map[string]TaskRepo, len(repos))
	for _, r := range repos {
		byName[r.Repo] = r
	}
	for _, e := range entries {
		r, ok := byName[e.Name()]
		if !ok || !alreadyMoved(r.WorktreePath, filepath.Join(newDir, e.Name())) {
			return fmt.Errorf("worktree directory %s already exists and contains %q, which is not a worktree of this task moved by an earlier `mv`", newDir, e.Name())
		}
	}
	return nil
}

// moveTicketDirLeftovers moves everything still in oldDir after the repo
// worktrees have been relocated (notes, env files, anything the user keeps
// at the ticket root) into newDir, then removes oldDir only if it ended up
// empty. Entries that would collide with something already in newDir are
// left in place, so nothing is ever deleted here.
func moveTicketDirLeftovers(oldDir, newDir string) error {
	entries, err := os.ReadDir(oldDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading old ticket workspace dir %s: %w", oldDir, err)
	}
	if len(entries) > 0 {
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", newDir, err)
		}
	}
	var failed []string
	for _, e := range entries {
		src := filepath.Join(oldDir, e.Name())
		dst := filepath.Join(newDir, e.Name())
		if _, err := os.Lstat(dst); err == nil {
			failed = append(failed, fmt.Sprintf("%s (already exists in %s)", e.Name(), newDir))
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", e.Name(), err))
			continue
		}
		fmt.Printf("moved %s -> %s\n", src, dst)
	}
	if len(failed) > 0 {
		return fmt.Errorf("left in old ticket workspace dir %s: %s", oldDir, strings.Join(failed, ", "))
	}
	if err := os.Remove(oldDir); err != nil {
		return fmt.Errorf("removing old ticket workspace dir %s: %w", oldDir, err)
	}
	return nil
}

// moveRepoWorktree relocates a single repo's worktree from r.WorktreePath to
// newWorktreePath, using `git worktree move` (not a plain filesystem rename)
// so git's own worktree administrative data stays consistent. For
// runtime: windows repos the real tree under windows_worktree_root moves and
// the WSL-side symlink is recreated (see windowsGit.WorktreeMove). cfg may be
// nil, or missing this repo's entry entirely (repos.yaml changed since the
// task was created); best-effort falls back to running the move from
// within the worktree itself rather than aborting, mirroring
// removeRepoWorktree's fallback in cmd_rm.go.
func moveRepoWorktree(cfg *ReposConfig, r TaskRepo, newWorktreePath string) error {
	runner, base, err := gitForExisting(cfg, r.Repo, r.WorktreePath, "worktree move")
	if err != nil {
		return err
	}
	return runner.WorktreeMove(base, r.WorktreePath, newWorktreePath)
}

// repoNames renders repos as a comma-separated list of repo names, for the
// partial-failure error message in cmdMv.
func repoNames(repos []TaskRepo) string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Repo
	}
	return strings.Join(names, ", ")
}
