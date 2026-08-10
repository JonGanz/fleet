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
func cmdEdit(ticket string) error {
	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	st, err := readTaskState(tf)
	if err != nil {
		return fmt.Errorf("read task %s: %w", ticket, err)
	}

	rf, err := reposFile()
	if err != nil {
		return err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return err
	}

	attachedNames := make([]string, len(st.Repos))
	for i, r := range st.Repos {
		attachedNames[i] = r.Repo
	}

	// selectMultiOptionalTitled, not selectMultiTitled: an empty selection is
	// a common, valid answer on both of these pickers (e.g. you may only be
	// here to remove a repo and add none, or vice versa), so a bare enter
	// with nothing checked must confirm "nothing" rather than falling back
	// to whatever repo the cursor happens to be on.
	toAdd, err := selectMultiOptionalTitled(fmt.Sprintf("%s: select repos to add", ticket), availableRepoNames(cfg, st.Repos))
	if err != nil {
		return fmt.Errorf("add-repo selection: %w", err)
	}
	toRemove, err := selectMultiOptionalTitled(fmt.Sprintf("%s: select repos to remove", ticket), attachedNames)
	if err != nil {
		return fmt.Errorf("remove-repo selection: %w", err)
	}

	if len(toAdd) == 0 && len(toRemove) == 0 {
		fmt.Fprintln(os.Stderr, "no changes selected")
		return nil
	}

	// Ask about every to-be-added repo's patches up front too, for the same
	// reason `new` does: front-load all interactive prompts before any
	// (potentially slow) git/hook work starts.
	patchesByRepo := make(map[string][]string, len(toAdd))
	for _, repoName := range toAdd {
		patches, err := selectPatches(repoName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: patch selection for %s: %v\n", repoName, err)
			continue
		}
		patchesByRepo[repoName] = patches
	}

	toRemoveSet := make(map[string]bool, len(toRemove))
	for _, name := range toRemove {
		toRemoveSet[name] = true
	}

	// Removals run first and are persisted before any additions start, so a
	// failure partway through additions doesn't lose already-successful
	// removals. Repos that fail to remove are kept in the task (same
	// partial-failure contract as `rm`), so a rerun of `edit` retries them.
	var kept []TaskRepo
	removedCount := 0
	removeFailures := 0
	for _, r := range st.Repos {
		if !toRemoveSet[r.Repo] {
			kept = append(kept, r)
			continue
		}
		if err := removeRepoWorktree(cfg, ticket, r); err != nil {
			fmt.Fprintf(os.Stderr, "warning: removing worktree %s: %v\n", r.WorktreePath, err)
			kept = append(kept, r)
			removeFailures++
			continue
		}
		fmt.Printf("removed worktree %s (%s)\n", r.WorktreePath, r.Repo)
		removedCount++
	}
	st.Repos = kept
	if removedCount > 0 {
		if err := writeTaskState(tf, st); err != nil {
			return fmt.Errorf("update task state %s after removal: %w", tf, err)
		}
	}

	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		return err
	}

	addedCount := 0
	for _, repoName := range toAdd {
		repo := cfg.findRepo(repoName)
		if repo == nil {
			fmt.Fprintf(os.Stderr, "warning: repo %q not found in config, skipping\n", repoName)
			continue
		}
		tr, err := createRepoWorktree(cfg, repo, ticket, st.Description, wtRoot, patchesByRepo[repoName])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		st.Repos = append(st.Repos, tr)
		addedCount++
	}

	// If every repo ended up removed and none were added, drop the task
	// file entirely rather than leaving an empty-repos state file behind --
	// matches `rm`'s end state for "nothing left to track."
	if len(st.Repos) == 0 {
		if err := os.Remove(tf); err != nil {
			return fmt.Errorf("remove task state %s: %w", tf, err)
		}
		fmt.Printf("task %s has no repos left; removed task state %s\n", ticket, tf)
		return nil
	}

	if err := writeTaskState(tf, st); err != nil {
		return fmt.Errorf("write task state: %w", err)
	}

	fmt.Printf("task %s updated: added %d, removed %d (%d removal failure(s)); state written to %s\n",
		ticket, addedCount, removedCount, removeFailures, tf)
	return nil
}
