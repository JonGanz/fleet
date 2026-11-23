package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/hooks"
	"github.com/JonGanz/fleet/internal/picker"
	"github.com/JonGanz/fleet/internal/taskstate"
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

// addRepos sets up a worktree for each named repo (createRepoWorktree),
// appending each success to st.Repos and writing st to tf right away, so
// the state file always lists every worktree that exists even if the
// command dies partway (Ctrl-C during a long npm ci). Every repo is
// attempted even after a failure; failed counts them (unknown repo, or
// worktree setup error). err is set only if the state write itself fails,
// which stops further work: carrying on would create worktrees nothing
// records. The caller must hold the ticket's lock.
func addRepos(cfg *config.ReposConfig, st *taskstate.State, tf, wtRoot string, repoNames []string, patchesByRepo map[string][]string) (results []repoResult, failed int, err error) {
	for _, repoName := range repoNames {
		repo := cfg.FindRepo(repoName)
		if repo == nil {
			err := fmt.Errorf("repo %q not found in config, skipping", repoName)
			cli.Errorf("%v", err)
			results = append(results, repoResult{Repo: repoName, Action: "created", Error: err.Error()})
			failed++
			continue
		}
		tr, patchErrors, adopted, err := createRepoWorktree(cfg, repo, st.Ticket, st.Description, wtRoot, patchesByRepo[repoName])
		if err != nil {
			cli.Errorf("%v", err)
			results = append(results, repoResult{Repo: repoName, Action: "created", Error: err.Error()})
			failed++
			continue
		}
		st.Repos = append(st.Repos, tr)
		if err := taskstate.Write(tf, st); err != nil {
			results = append(results, repoResult{Repo: tr.Repo, Action: "created", Branch: tr.Branch, WorktreePath: tr.WorktreePath, Patches: tr.Patches, Error: err.Error()})
			return results, failed + 1, fmt.Errorf("worktree for %s was created at %s but recording it in %s failed: %w", tr.Repo, tr.WorktreePath, tf, err)
		}
		results = append(results, repoResult{
			Repo:         tr.Repo,
			Action:       "created",
			OK:           true,
			Branch:       tr.Branch,
			WorktreePath: tr.WorktreePath,
			Patches:      tr.Patches,
			PatchErrors:  patchErrors,
			Adopted:      adopted,
		})
	}
	return results, failed, nil
}

func cmdNew(o newOpts) error {
	useJSONOutput(o.JSON)

	ticket, description, err := resolveNewTicket(o)
	if err != nil {
		return err
	}

	// taskFile validates the ticket id (usage error if unsafe).
	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	if err := requireNoTask(ticket, tf); err != nil {
		return err
	}
	// Held for the whole command, so a concurrent `new`/`edit`/`rm` of the
	// same ticket fails fast instead of interleaving with this one.
	unlock, err := lockTicket(tf)
	if err != nil {
		return err
	}
	defer unlock()
	if err := requireNoTask(ticket, tf); err != nil {
		return err
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
	if err := validateBranchNames(cfg, selectedRepos, ticket, description); err != nil {
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

	// addRepos writes the state file after every repo it sets up, so an
	// interrupted `new` still leaves a task that `edit`/`rm` can handle.
	st := &taskstate.State{
		Ticket:      ticket,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}
	results, failed, addErr := addRepos(cfg, st, tf, wtRoot, selectedRepos, patchesByRepo)
	report := func() error {
		if !o.JSON {
			return nil
		}
		return cli.PrintJSON(opResult{Ticket: ticket, OK: failed == 0 && addErr == nil, Repos: results})
	}
	if addErr != nil {
		if err := report(); err != nil {
			cli.Warnf("printing JSON result: %v", err)
		}
		return addErr
	}

	if len(st.Repos) == 0 {
		if err := report(); err != nil {
			return err
		}
		return fmt.Errorf("no repos were successfully set up; not writing task state")
	}

	fmt.Fprintf(out, "Task %s created with %d repo(s); state written to %s\n", ticket, len(st.Repos), tf)
	if err := report(); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d repo(s) failed to set up; the rest were recorded -- retry the failed ones with `fleet-task edit %s --add <repo>`", failed, len(selectedRepos), ticket)
	}
	return nil
}

// createRepoWorktree performs the full per-repo worktree setup, shared
// between `new` (every repo for a brand new task) and `edit`'s add-repo
// path (a subset of repos added to an already-existing task): pre-create
// hooks, then base clone/fetch and worktree creation/branch checkout (or
// adoption of a worktree an interrupted run left behind), patch application,
// post-create hooks, and node_modules cache priming. Note the order:
// pre-create hooks run *before* the base clone is ensured, so they can't
// assume the base exists.
//
// A failing pre-create hook cancels this repo: nothing else is done for it
// and the error is returned, like a failure setting up the worktree itself,
// so the caller can report it and keep going with the rest. Post-create hook
// and patch failures are only warned about. Applied patch names are recorded
// on the returned taskstate.Repo; patches that failed to apply are returned
// as patchErrors. An adopted worktree gets no patches applied (they may
// already be there), so its Patches is nil: unknown.
func createRepoWorktree(cfg *config.ReposConfig, repo *config.RepoConfig, ticket, description, wtRoot string, patches []string) (tr taskstate.Repo, patchErrors []string, adopted bool, err error) {
	worktreePath := filepath.Join(wtRoot, ticket, repo.Name)
	branch := branchName(repo.BranchTemplate, ticket, description)

	hook := hooks.Env{Ticket: ticket, Description: description, Repo: repo.Name, Branch: branch, WorktreeDir: worktreePath}
	if err := hooks.Run(hooks.PreCreate, hook, out, cfg.HookTimeout()); err != nil {
		return taskstate.Repo{}, nil, false, preHookError(repo.Name+" not set up", hooks.PreCreate, err)
	}

	runner, base, err := gitFor(cfg, repo)
	if err != nil {
		return taskstate.Repo{}, nil, false, fmt.Errorf("setting up worktree for %s: %w", repo.Name, err)
	}
	adopted, err = setupWorktree(runner, repo, base, branch, worktreePath)
	if err != nil {
		return taskstate.Repo{}, nil, false, fmt.Errorf("setting up worktree for %s: %w", repo.Name, err)
	}

	var applied []string
	if adopted {
		fmt.Fprintf(out, "adopted existing worktree %s (%s, branch %s); patches were not applied again and are recorded as unknown\n", worktreePath, repo.Name, branch)
	} else {
		applied = []string{}
		for _, p := range patches {
			if err := runner.ApplyPatch(worktreePath, p); err != nil {
				cli.Warnf("applying patch %s to %s: %v", p, repo.Name, err)
				patchErrors = append(patchErrors, fmt.Sprintf("%s: %v", filepath.Base(p), err))
				continue
			}
			applied = append(applied, filepath.Base(p))
		}
	}

	if err := hooks.Run(hooks.PostCreate, hook, out, cfg.HookTimeout()); err != nil {
		cli.Warnf("post-create hooks for %s: %v", repo.Name, err)
	}

	if _, err := os.Stat(filepath.Join(worktreePath, "package-lock.json")); err == nil {
		ensureNodeCache(repo, cfg, worktreePath)
	}

	return taskstate.Repo{
		Repo:         repo.Name,
		Branch:       branch,
		WorktreePath: worktreePath,
		Patches:      applied,
	}, patchErrors, adopted, nil
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

// requireNoTask refuses (usage error) a ticket that already has a state
// file: `new` would overwrite it with only this run's repos and reset
// created_at, orphaning the earlier worktrees.
func requireNoTask(ticket, tf string) error {
	if _, err := os.Stat(tf); err == nil {
		return cli.Usagef("task %s already exists (%s); use `fleet-task edit %s` to add or remove repos, or `fleet-task rm %s` to start over", ticket, tf, ticket, ticket)
	}
	return nil
}

// resolveNewRepos returns the repos to set up: the --repo values (validated
// against repos.yaml, deduplicated, order kept) if any, else the picker.
func resolveNewRepos(cfg *config.ReposConfig, flagRepos []string) ([]string, error) {
	if len(flagRepos) > 0 {
		repos := uniqueStrings(flagRepos)
		for _, name := range repos {
			if cfg.FindRepo(name) == nil {
				return nil, cli.Usagef("unknown repo %q (see `fleet-task repos`)", name)
			}
		}
		return repos, nil
	}
	if !cli.InteractiveAllowed() {
		return nil, needInput("at least one repo", "pass --repo <name> (see `fleet-task repos`)")
	}
	selected, err := picker.Run(picker.Options{Items: cfg.RepoNames(), Multi: true, CursorFallback: true})
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
	if !cli.InteractiveAllowed() {
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
	selectedNames, err := picker.Run(picker.Options{Title: title, Items: names, Multi: true, Preselected: true})
	if err != nil {
		return nil, err
	}

	selected := make([]string, len(selectedNames))
	for i, name := range selectedNames {
		selected[i] = pathByName[name]
	}
	return selected, nil
}

// selectPatchesFor asks about every named repo's patches up front, before
// any directory/worktree work starts, so all user interaction front-loads
// instead of interleaving prompts with (potentially slow) git/hook/cache
// work. Shared by `new` and `edit`'s add phase. A repo whose patch selection
// fails gets a warning and no patches.
func selectPatchesFor(cfg *config.ReposConfig, repoNames []string) map[string][]string {
	patchesByRepo := make(map[string][]string, len(repoNames))
	for _, repoName := range repoNames {
		if cfg.FindRepo(repoName) == nil {
			continue
		}
		patches, err := selectPatches(repoName)
		if err != nil {
			cli.Warnf("patch selection for %s: %v", repoName, err)
			continue
		}
		patchesByRepo[repoName] = patches
	}
	return patchesByRepo
}

// setupWorktree ensures the repo's base clone exists and is fetched
// (ensureBase, which also completes a base left half-made by an interrupted
// run) and creates the worktree at worktreePath on branch (addWorktree) --
// unless worktreePath already is a worktree of base on branch, left by an
// interrupted `new`/`edit --add`: that one is adopted instead (adopted is
// true; see decideAdoption). Anything else already at worktreePath is an
// error. Runtime differences (WSL git vs Windows-native git.exe plus a
// WSL-side symlink) live behind runner; base is the bare clone's
// WSL-visible path.
func setupWorktree(runner gitRunner, repo *config.RepoConfig, base, branch, worktreePath string) (adopted bool, err error) {
	if base == "" {
		return false, fmt.Errorf("repo %s has no base (set `base` directly or via defaults.base_root)", repo.Name)
	}
	if err := runner.Preflight(base, worktreePath); err != nil {
		return false, err
	}
	if err := ensureBase(runner, repo.Name, repo.Origin, base); err != nil {
		return false, err
	}
	// Prune first, so a registration whose directory is gone doesn't look
	// like something to adopt.
	if err := runner.Prune(base); err != nil {
		cli.Warnf("worktree prune for %s: %v", base, err)
	}
	at, err := runner.InspectWorktreePath(base, worktreePath)
	if err != nil {
		return false, fmt.Errorf("checking what is at %s: %w", worktreePath, err)
	}
	adopt, err := decideAdoption(at, worktreePath, base, branch)
	if err != nil {
		return false, err
	}
	if adopt {
		return true, runner.AdoptWorktree(worktreePath)
	}
	return false, addWorktree(runner, base, worktreePath, branch, repo.DefaultBranch)
}

// validateBranchNames computes every named repo's branch (branchName) and
// rejects, as a usage error before any git/hook work, one that is empty or
// that git won't accept as a branch name -- e.g. a template whose only
// placeholder is {description} with no description given.
func validateBranchNames(cfg *config.ReposConfig, repoNames []string, ticket, description string) error {
	for _, name := range repoNames {
		repo := cfg.FindRepo(name)
		if repo == nil {
			continue // reported per repo by addRepos
		}
		branch := branchName(repo.BranchTemplate, ticket, description)
		if branch == "" {
			return cli.Usagef("repo %s: branch_template %q gives an empty branch name for ticket %s and description %q", repo.Name, repo.BranchTemplate, ticket, description)
		}
		ok, err := isValidBranchName(branch)
		if err != nil {
			return err
		}
		if !ok {
			return cli.Usagef("repo %s: branch_template %q gives %q, which git does not accept as a branch name (`git check-ref-format --branch`)", repo.Name, repo.BranchTemplate, branch)
		}
	}
	return nil
}
