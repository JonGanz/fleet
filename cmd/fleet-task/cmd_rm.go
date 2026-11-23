package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/hooks"
	"github.com/JonGanz/fleet/internal/taskstate"
)

// Local-branch handling after a repo's worktree is removed (rm, edit
// --remove); see cleanupBranch.
const (
	branchAuto   = "auto"   // delete only if every commit is on a remote (default)
	branchKeep   = "keep"   // --keep-branch
	branchDelete = "delete" // --delete-branch: delete even with unpushed commits
)

// branchOutcome is what happened to one removed repo's local branch.
type branchOutcome struct {
	Deleted    bool
	KeptReason string
	// Warn: the branch was kept for a reason the user should look at
	// (unpushed commits, a failure), not by request or because it was
	// already gone.
	Warn bool
}

// cmdRm removes a task. force skips the uncommitted-changes guard
// (checkRemovable) and --delete-branch's confirmation; branchMode says what
// to do with each repo's local branch afterwards.
func cmdRm(ticket string, force bool, branchMode string, jsonOut bool) error {
	useJSONOutput(jsonOut)
	if ticket == "" {
		if !cli.InteractiveAllowed() {
			return needInput("a ticket id", "pass it as `fleet-task rm <ticket>`")
		}
		t, err := pickTicket()
		if err != nil {
			return err
		}
		ticket = t
	}

	tf, st, unlock, err := lockAndReadTask(ticket)
	if err != nil {
		return err
	}
	defer unlock()

	// Catch the "still running under fleet-run" case up front with an
	// actionable error instead of racing it and hoping selfHealWorktreeRemove
	// covers whatever's left.
	if err := requireNotActive(ticket, "removing it"); err != nil {
		return err
	}

	// repos.yaml is optional here: if it's missing or no longer has the
	// repo entry, we still try to remove the worktree (best-effort, via
	// git run from the worktree itself) rather than aborting the whole rm.
	cfg := loadReposConfigOptional()

	// Every repo is checked before any is removed, so a refusal leaves the
	// task exactly as it was.
	if err := checkRemovable(cfg, st.Repos, force); err != nil {
		return err
	}
	if err := checkBranchDeletion(cfg, st.Repos, branchMode, force); err != nil {
		return err
	}

	var remaining []taskstate.Repo
	results := []repoResult{}

	for _, r := range st.Repos {
		res := repoResult{Repo: r.Repo, Action: "removed", Branch: r.Branch, WorktreePath: r.WorktreePath, Patches: r.Patches}
		branch, rmErr := removeRepoWorktree(cfg, st, r, branchMode)
		if rmErr != nil {
			cli.Warnf("removing worktree %s: %v", r.WorktreePath, rmErr)
			remaining = append(remaining, r)
			res.Error = rmErr.Error()
			results = append(results, res)
			continue
		}
		res.OK = true
		res.setBranchOutcome(branch)
		results = append(results, res)
	}
	if jsonOut {
		// Printed before the state-file bookkeeping below: the per-repo
		// outcomes are final at this point, and stdout must carry the JSON
		// even when the command goes on to fail.
		if err := cli.PrintJSON(opResult{Ticket: ticket, OK: len(remaining) == 0, Repos: results}); err != nil {
			return err
		}
	}

	// If any worktree removal failed, keep the task state around (with only
	// the still-failing repos listed) instead of deleting it, so a rerun of
	// `rm` retries just those and the ticket doesn't silently vanish from
	// `fleet-task list`/`fleet-run` while its worktrees still exist on disk.
	if len(remaining) > 0 {
		st.Repos = remaining
		if err := taskstate.Write(tf, st); err != nil {
			return fmt.Errorf("update task state %s after partial removal: %w", tf, err)
		}
		return fmt.Errorf("%d repo(s) failed to remove for %s; task state kept at %s for retry", len(remaining), ticket, tf)
	}

	if err := os.Remove(tf); err != nil {
		return fmt.Errorf("remove task state %s: %w", tf, err)
	}
	fmt.Fprintf(out, "removed task state %s\n", tf)

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
		cli.Warnf("resolving worktree root to remove ticket workspace dir: %v", err)
		return nil
	}
	ticketDir := filepath.Join(wtRoot, ticket)
	removed, leftovers, err := removeDirIfEmpty(ticketDir)
	switch {
	case err != nil:
		cli.Warnf("removing ticket workspace dir %s: %v", ticketDir, err)
	case removed:
		fmt.Fprintf(out, "removed ticket workspace dir %s\n", ticketDir)
	case len(leftovers) > 0:
		cli.Warnf("left ticket workspace dir %s in place; it still contains: %s", ticketDir, strings.Join(leftovers, ", "))
	}
	return nil
}

// removeRepoWorktree tears down a single repo's worktree, shared between
// `rm` (removing every repo in a task) and `edit`'s remove-repo path
// (removing a selected subset). cfg may be nil, or missing this repo's
// entry entirely (repos.yaml changed, or the repo was dropped from it,
// since the task was created) -- best-effort falls back to a bare
// `git worktree remove` run against the worktree itself rather than
// aborting. pre-remove hooks run first: a failing one cancels this repo's
// removal (the error is returned and the repo stays in the task). Once the
// worktree is gone its local branch is dealt with per branchMode
// (cleanupBranch), then post-remove hooks run; their failures only warn.
func removeRepoWorktree(cfg *config.ReposConfig, st *taskstate.State, r taskstate.Repo, branchMode string) (branchOutcome, error) {
	hook := hooks.Env{Ticket: st.Ticket, Description: st.Description, Repo: r.Repo, Branch: r.Branch, WorktreeDir: r.WorktreePath}
	if err := hooks.Run(hooks.PreRemove, hook, out, cfg.HookTimeout()); err != nil {
		return branchOutcome{}, preHookError(r.Repo+" not removed", hooks.PreRemove, err)
	}

	runner, base, baseKnown, err := resolveExistingGit(cfg, r.Repo, r.WorktreePath)
	if err != nil {
		return branchOutcome{}, err
	}
	if !baseKnown {
		cli.Warnf("repo %q not found in repos.yaml (or has no base), trying worktree remove without a base", r.Repo)
	}
	removeErr := selfHealWorktreeRemove(r.WorktreePath, runner.WorktreeRemove(base, r.WorktreePath), func() error {
		return runner.Prune(base)
	})
	if removeErr != nil {
		return branchOutcome{}, removeErr
	}
	fmt.Fprintf(out, "removed worktree %s (%s)\n", r.WorktreePath, r.Repo)

	branch := cleanupBranch(runner, base, baseKnown, r.Branch, branchMode)
	reportBranchOutcome(r, base, branch)

	if err := hooks.Run(hooks.PostRemove, hook, out, cfg.HookTimeout()); err != nil {
		cli.Warnf("post-remove hooks for %s: %v", r.Repo, err)
	}
	return branch, nil
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
			cli.Warnf("worktree prune after removing missing %s: %v", worktreePath, pruneErr)
		}
		cli.Warnf("%s was already gone from disk; pruned its worktree registration", worktreePath)
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
	cli.Warnf("git had already deregistered %s; removed leftover directory", worktreePath)
	return nil
}

// worktreeAdminGone reports whether worktreePath no longer has a .git entry
// inside it, meaning git has already fully torn down its worktree
// registration and tracked content -- see selfHealWorktreeRemove.
func worktreeAdminGone(worktreePath string) bool {
	_, err := os.Lstat(filepath.Join(worktreePath, ".git"))
	return os.IsNotExist(err)
}

// checkBranchDeletion is --delete-branch's guard, run with checkRemovable
// before anything is removed: it lists every branch about to be deleted
// that has commits on no remote (or can't be checked) and asks for
// confirmation on a terminal; without one it is a usage error naming
// --force. force skips it, like the changes guard. Branches that will be
// kept anyway (base unknown, branch missing) are not asked about.
func checkBranchDeletion(cfg *config.ReposConfig, repos []taskstate.Repo, branchMode string, force bool) error {
	if branchMode != branchDelete || force {
		return nil
	}
	var problems []string
	for _, r := range repos {
		runner, base, baseKnown, err := resolveExistingGit(cfg, r.Repo, r.WorktreePath)
		if err != nil || !baseKnown || !usableBranchName(r.Branch) {
			continue // removal reports err; the branch is kept in the others
		}
		exists, err := runner.RefExists(base, "refs/heads/"+r.Branch)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: branch %s could not be checked for unpushed commits: %v", r.Repo, r.Branch, err))
			continue
		}
		if !exists {
			continue
		}
		n, err := runner.UnpushedInBase(base, r.Branch)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: branch %s could not be checked for unpushed commits: %v", r.Repo, r.Branch, err))
		case n > 0:
			problems = append(problems, fmt.Sprintf("%s: branch %s has %d commit(s) that are on no remote", r.Repo, r.Branch, n))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	for _, p := range problems {
		cli.Warnf("%s", p)
	}
	if !cli.InteractiveAllowed() {
		return cli.Usagef("--delete-branch would delete %d branch(es) with unpushed or unchecked commits (see warnings above); nothing was removed -- push them first, pass --force as well to delete them anyway, or drop --delete-branch to keep unpushed branches", len(problems))
	}
	ok, err := confirm("Delete these branches anyway, losing those commits? [y/N] ")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("aborted; nothing was removed")
	}
	return nil
}

// cleanupBranch deals with a removed worktree's local branch in base, per
// branchMode: branchKeep keeps it; branchAuto deletes it only when every
// commit on it is reachable from a remote-tracking ref (`rev-list --count
// <branch> --not --remotes` is 0; no fetch, so a remote ref deleted upstream
// but still known locally counts as pushed); branchDelete deletes it
// regardless (checkBranchDeletion has already asked). The branch is always
// kept when base is unknown (the gitForExisting fallback) or the recorded
// name is unusable; a missing branch is fine. git's own refusal (the branch
// is checked out in another worktree) keeps it and is reported, never
// failing the removal.
func cleanupBranch(runner gitRunner, base string, baseKnown bool, branch, branchMode string) branchOutcome {
	kept := func(warn bool, format string, args ...any) branchOutcome {
		return branchOutcome{KeptReason: fmt.Sprintf(format, args...), Warn: warn}
	}
	switch {
	case branchMode == branchKeep:
		return kept(false, "--keep-branch given")
	case !baseKnown:
		return kept(true, "the repo's base is unknown (repo missing from repos.yaml, or it has no base), so the branch could not be checked")
	case !usableBranchName(branch):
		return kept(true, "the recorded branch name %q is not usable", branch)
	}
	exists, err := runner.RefExists(base, "refs/heads/"+branch)
	if err != nil {
		return kept(true, "could not check for the branch: %v", err)
	}
	if !exists {
		return kept(false, "no local branch %s in the base (nothing to delete)", branch)
	}
	if branchMode != branchDelete {
		n, err := runner.UnpushedInBase(base, branch)
		if err != nil {
			return kept(true, "could not count its unpushed commits: %v", err)
		}
		if n > 0 {
			return kept(true, "%d commit(s) on it are on no remote", n)
		}
	}
	if err := runner.DeleteBranch(base, branch); err != nil {
		return kept(true, "git did not delete it: %v", err)
	}
	return branchOutcome{Deleted: true}
}

// reportBranchOutcome prints what happened to r's branch: a progress line
// when it was deleted or deliberately kept, a warning (with the command to
// delete it by hand) when it was kept for a reason worth a look.
func reportBranchOutcome(r taskstate.Repo, base string, o branchOutcome) {
	switch {
	case o.Deleted:
		fmt.Fprintf(out, "deleted branch %s (%s)\n", r.Branch, r.Repo)
	case o.Warn && usableBranchName(r.Branch):
		cli.Warnf("kept branch %s (%s): %s; to delete it anyway: git -C %s branch -D %s", r.Branch, r.Repo, o.KeptReason, base, r.Branch)
	case o.Warn:
		cli.Warnf("kept branch %s (%s): %s", r.Branch, r.Repo, o.KeptReason)
	default:
		fmt.Fprintf(out, "kept branch %s (%s): %s\n", r.Branch, r.Repo, o.KeptReason)
	}
}

// setBranchOutcome records o on a successful removal's result.
func (res *repoResult) setBranchOutcome(o branchOutcome) {
	deleted := o.Deleted
	res.BranchDeleted = &deleted
	res.BranchKeptReason = o.KeptReason
}

// usableBranchName rejects a recorded branch name fleet-task must not pass
// to `git branch -D`: empty, or one git could read as an option.
func usableBranchName(branch string) bool {
	return branch != "" && !strings.HasPrefix(branch, "-")
}
