package main

import (
	"fmt"
	"os"
)

// availableRepoNames returns cfg's repo names that aren't already attached
// to the task (per attached, its current TaskRepo list), preserving
// repos.yaml's ordering -- the candidate list for `edit`'s add-repo picker.
func availableRepoNames(cfg *ReposConfig, attached []TaskRepo) []string {
	inTask := make(map[string]bool, len(attached))
	for _, r := range attached {
		inTask[r.Repo] = true
	}
	var available []string
	for _, name := range cfg.repoNames() {
		if !inTask[name] {
			available = append(available, name)
		}
	}
	return available
}

// cmdEdit lets an already-created task's repo list grow or shrink: repos
// not yet part of the task can be added (full worktree setup, same as
// `new`, including pre-create/post-create hooks), and repos already part of
// it can be removed (same teardown as `rm`, including pre-remove/post-remove
// hooks, minus deleting the task file itself unless every repo ends up
// removed). Both add/remove selections are made up front, before any
// git/hook work starts, mirroring `new`'s patch-selection front-loading.
// Every selected repo is attempted even if some fail; failures make the
// command exit non-zero once everything has been tried and state saved.
func cmdEdit(ticket string) error {
	if ticket == "" {
		picked, err := pickTicket()
		if err != nil {
			return err
		}
		ticket = picked
	}

	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	st, err := readTaskState(tf)
	if err != nil {
		return fmt.Errorf("read task %s: %w", ticket, err)
	}

	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	toAdd, toRemove, err := selectEditRepos(cfg, ticket, st)
	if err != nil {
		return err
	}
	if len(toAdd) == 0 && len(toRemove) == 0 {
		fmt.Println("no changes selected")
		return nil
	}

	// Removing a repo's worktree while its dev server is still running under
	// fleet-run for this exact ticket can leave untracked cruft behind git's
	// own cleanup (see selfHealWorktreeRemove in cmd_rm.go). Adding repos is
	// unaffected, so only check when a removal is actually requested.
	if len(toRemove) > 0 {
		if err := requireNotActive(ticket, "removing repos from it"); err != nil {
			return err
		}
	}

	// Ask about every to-be-added repo's patches up front too, for the same
	// reason `new` does: front-load all interactive prompts before any
	// (potentially slow) git/hook work starts.
	patchesByRepo := selectPatchesFor(cfg, toAdd)

	removed, removeFailures, err := editRemovePhase(cfg, ticket, tf, st, toRemove)
	if err != nil {
		return err
	}
	added, addFailures, err := editAddPhase(cfg, ticket, st, toAdd, patchesByRepo)
	if err != nil {
		return err
	}
	failures := removeFailures + addFailures

	// If every repo ended up removed and none were added, drop the task
	// file entirely rather than leaving an empty-repos state file behind --
	// matches `rm`'s end state for "nothing left to track."
	if len(st.Repos) == 0 {
		if err := os.Remove(tf); err != nil {
			return fmt.Errorf("remove task state %s: %w", tf, err)
		}
		fmt.Printf("task %s has no repos left; removed task state %s\n", ticket, tf)
	} else {
		if err := writeTaskState(tf, st); err != nil {
			return fmt.Errorf("write task state: %w", err)
		}
		fmt.Printf("task %s updated: added %d, removed %d (%d failure(s)); state written to %s\n",
			ticket, added, removed, failures, tf)
	}

	if failures > 0 {
		return fmt.Errorf("%d repo operation(s) failed for %s (see messages above); rerun `fleet-task edit %s` to retry", failures, ticket, ticket)
	}
	return nil
}

// selectEditRepos shows the add and remove repo pickers and returns the
// chosen repo names. An empty selection is a common, valid answer on both
// (you may only be here to remove a repo and add none, or vice versa), so
// CursorFallback is off: a bare enter with nothing checked confirms "nothing"
// rather than whatever repo the cursor happens to be on.
func selectEditRepos(cfg *ReposConfig, ticket string, st *TaskState) (toAdd, toRemove []string, err error) {
	attachedNames := make([]string, len(st.Repos))
	for i, r := range st.Repos {
		attachedNames[i] = r.Repo
	}

	toAdd, err = runSelect(selectOpts{
		Title: fmt.Sprintf("%s: select repos to add", ticket),
		Items: availableRepoNames(cfg, st.Repos),
		Multi: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("add-repo selection: %w", err)
	}
	toRemove, err = runSelect(selectOpts{
		Title: fmt.Sprintf("%s: select repos to remove", ticket),
		Items: attachedNames,
		Multi: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("remove-repo selection: %w", err)
	}
	return toAdd, toRemove, nil
}

// editRemovePhase removes the selected repos' worktrees, updating st.Repos
// in place and persisting it before any additions start, so a failure
// partway through additions doesn't lose already-successful removals. Repos
// that fail to remove stay in the task (same partial-failure contract as
// `rm`), so a rerun of `edit` retries them.
func editRemovePhase(cfg *ReposConfig, ticket, tf string, st *TaskState, toRemove []string) (removed, failures int, err error) {
	toRemoveSet := make(map[string]bool, len(toRemove))
	for _, name := range toRemove {
		toRemoveSet[name] = true
	}

	var kept []TaskRepo
	for _, r := range st.Repos {
		if !toRemoveSet[r.Repo] {
			kept = append(kept, r)
			continue
		}
		if err := removeRepoWorktree(cfg, ticket, r); err != nil {
			warnf("removing worktree %s: %v", r.WorktreePath, err)
			kept = append(kept, r)
			failures++
			continue
		}
		fmt.Printf("removed worktree %s (%s)\n", r.WorktreePath, r.Repo)
		removed++
	}
	st.Repos = kept
	if removed > 0 {
		if err := writeTaskState(tf, st); err != nil {
			return removed, failures, fmt.Errorf("update task state %s after removal: %w", tf, err)
		}
	}
	return removed, failures, nil
}

// editAddPhase sets up worktrees for the selected repos (same per-repo setup
// as `new`, using the task's existing description so branch names stay
// consistent with the repos added at `new` time), appending the successes to
// st.Repos. Returns how many were added and how many failed.
func editAddPhase(cfg *ReposConfig, ticket string, st *TaskState, toAdd []string, patchesByRepo map[string][]string) (added, failures int, err error) {
	if len(toAdd) == 0 {
		return 0, 0, nil
	}
	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		return 0, 0, err
	}
	newRepos, failures := addRepos(cfg, ticket, st.Description, wtRoot, toAdd, patchesByRepo)
	st.Repos = append(st.Repos, newRepos...)
	return len(newRepos), failures, nil
}
