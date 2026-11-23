package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// newOpts holds `fleet-task new`'s arguments. Giving --repo switches the
// whole command to flag mode: no picker, and patches come from
// --skip-patch/--no-patches (default: all of them, like the picker's
// all-checked default) instead of a prompt.
type newOpts struct {
	Ticket         string
	Description    string
	DescriptionSet bool
	Repos          []string
	SkipPatches    []string
	NoPatches      bool
	JSON           bool
}

func cmdNew(o newOpts) error {
	useJSONOutput(o.JSON)

	ticket, description, err := resolveNewTicket(o)
	if err != nil {
		return err
	}

	// taskFile validates the ticket id (no path separators, not "." / "..").
	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	// `new` on an existing ticket would overwrite its state with only this
	// run's repos and reset created_at, orphaning the earlier worktrees.
	if _, err := os.Stat(tf); err == nil {
		return usageErrorf("task %s already exists (%s); use `fleet-task edit %s` to add or remove repos, or `fleet-task rm %s` to start over", ticket, tf, ticket, ticket)
	}

	cfg, rf, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Repos) == 0 {
		return fmt.Errorf("no repos defined in %s", rf)
	}

	flagMode := len(o.Repos) > 0
	selectedRepos, err := resolveNewRepos(cfg, o.Repos)
	if err != nil {
		return err
	}

	var patchesByRepo map[string][]string
	if flagMode || o.NoPatches || len(o.SkipPatches) > 0 {
		patchesByRepo, err = patchesFromFlags(selectedRepos, o.SkipPatches, o.NoPatches)
		if err != nil {
			return err
		}
	} else {
		patchesByRepo = selectPatchesFor(cfg, selectedRepos)
	}

	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		return err
	}

	taskRepos, results, failed := addRepos(cfg, ticket, description, wtRoot, selectedRepos, patchesByRepo)
	report := func() error {
		if !o.JSON {
			return nil
		}
		return printJSON(opResult{Ticket: ticket, OK: failed == 0, Repos: results})
	}

	if len(taskRepos) == 0 {
		if err := report(); err != nil {
			return err
		}
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

	fmt.Fprintf(out, "Task %s created with %d repo(s); state written to %s\n", ticket, len(taskRepos), tf)
	if err := report(); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d repo(s) failed to set up; the rest were recorded -- retry the failed ones with `fleet-task edit %s --add <repo>`", failed, len(selectedRepos), ticket)
	}
	return nil
}

// patchesFromFlags lists each repo's available patches and applies
// --skip-patch/--no-patches to them (see resolvePatches).
func patchesFromFlags(repoNames, skip []string, noPatches bool) (map[string][]string, error) {
	available := make(map[string][]string, len(repoNames))
	for _, repo := range repoNames {
		paths, err := listPatchPaths(repo)
		if err != nil {
			return nil, fmt.Errorf("listing patches for %s: %w", repo, err)
		}
		available[repo] = paths
	}
	return resolvePatches(repoNames, available, skip, noPatches)
}

// resolveNewRepos returns the repos to set up: the --repo values (validated
// against repos.yaml, deduplicated, order kept) if any, else the picker.
func resolveNewRepos(cfg *ReposConfig, flagRepos []string) ([]string, error) {
	if len(flagRepos) > 0 {
		repos := uniqueStrings(flagRepos)
		for _, name := range repos {
			if cfg.findRepo(name) == nil {
				return nil, usageErrorf("unknown repo %q (see `fleet-task repos`)", name)
			}
		}
		return repos, nil
	}
	if !interactiveAllowed() {
		return nil, needInput("at least one repo", "pass --repo <name> (see `fleet-task repos`)")
	}
	selected, err := runSelect(selectOpts{Items: cfg.repoNames(), Multi: true, CursorFallback: true})
	if err != nil {
		return nil, fmt.Errorf("repo selection: %w", err)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no repos selected")
	}
	return selected, nil
}

// resolveNewTicket returns the ticket id and description: from arguments
// when the ticket was given, else from the interactive form (asking for the
// description too unless --description was given).
func resolveNewTicket(o newOpts) (ticket, description string, err error) {
	if o.Ticket != "" {
		return o.Ticket, o.Description, nil
	}
	if !interactiveAllowed() {
		return "", "", needInput("a ticket id", "pass it as `fleet-task new <ticket> --repo <name> [-d <description>]`")
	}
	labels := []string{"Ticket", "Description"}
	if o.DescriptionSet {
		labels = labels[:1]
	}
	values, err := promptForm(labels)
	if err != nil {
		return "", "", fmt.Errorf("read ticket/description: %w", err)
	}
	if o.DescriptionSet {
		return values[0], o.Description, nil
	}
	return values[0], values[1], nil
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
func addRepos(cfg *ReposConfig, ticket, description, wtRoot string, repoNames []string, patchesByRepo map[string][]string) (added []TaskRepo, results []repoResult, failed int) {
	for _, repoName := range repoNames {
		repo := cfg.findRepo(repoName)
		if repo == nil {
			err := fmt.Errorf("repo %q not found in config, skipping", repoName)
			errorf("%v", err)
			results = append(results, repoResult{Repo: repoName, Action: "created", Error: err.Error()})
			failed++
			continue
		}
		tr, patchErrors, err := createRepoWorktree(cfg, repo, ticket, description, wtRoot, patchesByRepo[repoName])
		if err != nil {
			errorf("%v", err)
			results = append(results, repoResult{Repo: repoName, Action: "created", Error: err.Error()})
			failed++
			continue
		}
		added = append(added, tr)
		results = append(results, repoResult{
			Repo:         tr.Repo,
			Action:       "created",
			OK:           true,
			Branch:       tr.Branch,
			WorktreePath: tr.WorktreePath,
			Patches:      tr.Patches,
			PatchErrors:  patchErrors,
		})
	}
	return added, results, failed
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
// Applied patch names are recorded on the returned TaskRepo; patches that
// failed to apply are returned as patchErrors.
func createRepoWorktree(cfg *ReposConfig, repo *RepoConfig, ticket, description, wtRoot string, patches []string) (tr TaskRepo, patchErrors []string, err error) {
	worktreePath := filepath.Join(wtRoot, ticket, repo.Name)
	branch := branchName(repo.BranchTemplate, ticket, description)

	if err := runHooks("pre-create", ticket, repo.Name, worktreePath); err != nil {
		warnf("pre-create hooks for %s: %v", repo.Name, err)
	}

	runner, base, err := gitFor(cfg, repo)
	if err != nil {
		return TaskRepo{}, nil, fmt.Errorf("setting up worktree for %s: %w", repo.Name, err)
	}
	if err := setupWorktree(runner, repo, base, branch, worktreePath); err != nil {
		return TaskRepo{}, nil, fmt.Errorf("setting up worktree for %s: %w", repo.Name, err)
	}

	applied := []string{}
	for _, p := range patches {
		if err := runner.ApplyPatch(worktreePath, p); err != nil {
			warnf("applying patch %s to %s: %v", p, repo.Name, err)
			patchErrors = append(patchErrors, fmt.Sprintf("%s: %v", filepath.Base(p), err))
			continue
		}
		applied = append(applied, filepath.Base(p))
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
		Patches:      applied,
	}, patchErrors, nil
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
	matches, err := listPatchPaths(repo)
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
