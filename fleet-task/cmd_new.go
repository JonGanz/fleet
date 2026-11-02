package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func cmdNew() error {
	values, err := promptForm([]string{"Ticket", "Description"})
	if err != nil {
		return fmt.Errorf("read ticket/description: %w", err)
	}
	ticket, description := values[0], values[1]

	// taskFile validates the ticket id (no path separators, not "." / "..").
	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	// `new` on an existing ticket would overwrite its state with only this
	// run's repos and reset created_at, orphaning the earlier worktrees.
	if _, err := os.Stat(tf); err == nil {
		return fmt.Errorf("task %s already exists (%s); use `fleet-task edit %s` to add or remove repos, or `fleet-task rm %s` to start over", ticket, tf, ticket, ticket)
	}

	cfg, rf, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Repos) == 0 {
		return fmt.Errorf("no repos defined in %s", rf)
	}

	selectedRepos, err := runSelect(selectOpts{Items: cfg.repoNames(), Multi: true, CursorFallback: true})
	if err != nil {
		return fmt.Errorf("repo selection: %w", err)
	}
	if len(selectedRepos) == 0 {
		return fmt.Errorf("no repos selected")
	}

	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		return err
	}

	patchesByRepo := selectPatchesFor(cfg, selectedRepos)
	taskRepos, failed := addRepos(cfg, ticket, description, wtRoot, selectedRepos, patchesByRepo)

	if len(taskRepos) == 0 {
		return fmt.Errorf("no repos were successfully set up; not writing task state")
	}

	st := &TaskState{
		Ticket:      ticket,
		Description: description,
		CreatedAt:   time.Now().UTC(),
		Repos:       taskRepos,
	}
	if err := writeTaskState(tf, st); err != nil {
		return fmt.Errorf("write task state: %w", err)
	}

	fmt.Printf("Task %s created with %d repo(s); state written to %s\n", ticket, len(taskRepos), tf)
	if failed > 0 {
		return fmt.Errorf("%d of %d repo(s) failed to set up; the rest were recorded -- retry the failed ones with `fleet-task edit %s`", failed, len(selectedRepos), ticket)
	}
	return nil
}

// selectPatchesFor asks about every named repo's patches up front, before
// any directory/worktree work starts, so all user interaction front-loads
// instead of interleaving prompts with (potentially slow) git/hook/cache
// work. Shared by `new` and `edit`'s add phase. A repo whose patch selection
// fails gets a warning and no patches.
func selectPatchesFor(cfg *ReposConfig, repoNames []string) map[string][]string {
	patchesByRepo := make(map[string][]string, len(repoNames))
	for _, repoName := range repoNames {
		if cfg.findRepo(repoName) == nil {
			continue
		}
		patches, err := selectPatches(repoName)
		if err != nil {
			warnf("patch selection for %s: %v", repoName, err)
			continue
		}
		patchesByRepo[repoName] = patches
	}
	return patchesByRepo
}

// addRepos sets up a worktree for each named repo (createRepoWorktree) and
// returns the ones that succeeded plus a count of failures (unknown repo, or
// worktree setup error). Every repo is attempted even after a failure; the
// caller decides how to report a nonzero failure count.
func addRepos(cfg *ReposConfig, ticket, description, wtRoot string, repoNames []string, patchesByRepo map[string][]string) (added []TaskRepo, failed int) {
	for _, repoName := range repoNames {
		repo := cfg.findRepo(repoName)
		if repo == nil {
			errorf("repo %q not found in config, skipping", repoName)
			failed++
			continue
		}
		tr, err := createRepoWorktree(cfg, repo, ticket, description, wtRoot, patchesByRepo[repoName])
		if err != nil {
			errorf("%v", err)
			failed++
			continue
		}
		added = append(added, tr)
	}
	return added, failed
}

// createRepoWorktree performs the full per-repo worktree setup, shared
// between `new` (every repo for a brand new task) and `edit`'s add-repo
// path (a subset of repos added to an already-existing task): pre-create
// hook, then base clone/fetch and worktree creation/branch checkout, patch
// application, post-create hook, and node_modules cache priming. Note the
// order: pre-create hooks run *before* the base clone is ensured, so they
// can't assume the base exists. Hook and patch failures are non-fatal
// (warned to stderr); a failure setting up the worktree itself is returned
// as an error so the caller can skip this repo and keep going with the rest.
func createRepoWorktree(cfg *ReposConfig, repo *RepoConfig, ticket, description, wtRoot string, patches []string) (TaskRepo, error) {
	worktreePath := filepath.Join(wtRoot, ticket, repo.Name)
	branch := branchName(repo.BranchTemplate, ticket, description)

	if err := runHooks("pre-create", ticket, repo.Name, worktreePath); err != nil {
		warnf("pre-create hooks for %s: %v", repo.Name, err)
	}

	runner, base, err := gitFor(cfg, repo)
	if err != nil {
		return TaskRepo{}, fmt.Errorf("setting up worktree for %s: %w", repo.Name, err)
	}
	if err := setupWorktree(runner, repo, base, branch, worktreePath); err != nil {
		return TaskRepo{}, fmt.Errorf("setting up worktree for %s: %w", repo.Name, err)
	}

	for _, p := range patches {
		if err := runner.ApplyPatch(worktreePath, p); err != nil {
			warnf("applying patch %s to %s: %v", p, repo.Name, err)
		}
	}

	if err := runHooks("post-create", ticket, repo.Name, worktreePath); err != nil {
		warnf("post-create hooks for %s: %v", repo.Name, err)
	}

	if _, err := os.Stat(filepath.Join(worktreePath, "package-lock.json")); err == nil {
		ensureNodeCache(repo, cfg, worktreePath)
	}

	return TaskRepo{
		Repo:         repo.Name,
		Branch:       branch,
		WorktreePath: worktreePath,
	}, nil
}

// selectPatches globs <config dir>/patches/<repo>/*.patch and, if any
// exist, lets the user multiselect which to apply, starting with every
// patch checked so the common case ("apply them all") needs no input —
// the user unchecks the ones they don't want. If there are zero patches
// for the repo, selection is skipped entirely and an empty slice is
// returned.
//
// The picker displays just each patch's filename, not its full
// <config dir>/patches/<repo>/... path, and is titled with the repo name so
// it's clear which repo's patches are being chosen -- the full path is only
// used internally to map the chosen filenames back to appliable paths.
func selectPatches(repo string) ([]string, error) {
	dir, err := patchesDir(repo)
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.patch"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	pathByName := make(map[string]string, len(matches))
	names := make([]string, len(matches))
	for i, m := range matches {
		name := filepath.Base(m)
		names[i] = name
		pathByName[name] = m
	}

	title := fmt.Sprintf("%s: select patches to apply", repo)
	selectedNames, err := runSelect(selectOpts{Title: title, Items: names, Multi: true, Preselected: true})
	if err != nil {
		return nil, err
	}

	selected := make([]string, len(selectedNames))
	for i, name := range selectedNames {
		selected[i] = pathByName[name]
	}
	return selected, nil
}

// setupWorktree ensures the repo's base bare clone exists (cloning or
// fetching as needed) and creates the worktree at worktreePath on branch.
// Runtime differences (WSL git vs Windows-native git.exe plus a WSL-side
// symlink) live behind runner; base is the bare clone's WSL-visible path.
func setupWorktree(runner gitRunner, repo *RepoConfig, base, branch, worktreePath string) error {
	if base == "" {
		return fmt.Errorf("repo %s has no base (set `base` directly or via defaults.base_root)", repo.Name)
	}
	if err := runner.Preflight(base, worktreePath); err != nil {
		return err
	}

	exists, err := runner.BaseExists(base)
	if err != nil {
		return fmt.Errorf("checking base %s: %w", base, err)
	}
	if !exists {
		if err := runner.CloneBare(repo.Origin, base); err != nil {
			return fmt.Errorf("clone %s: %w", repo.Origin, err)
		}
	} else if err := runner.Fetch(base); err != nil {
		return fmt.Errorf("fetch %s: %w", base, err)
	}

	if err := runner.Prune(base); err != nil {
		warnf("worktree prune for %s: %v", base, err)
	}

	// WorktreeAdd falls back from "create branch" to "attach existing
	// branch" (a prior run for this ticket) and reports both errors if both
	// fail.
	if err := runner.WorktreeAdd(base, worktreePath, branch, repo.DefaultBranch); err != nil {
		return err
	}
	return nil
}
