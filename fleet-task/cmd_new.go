package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdNew() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Ticket: ")
	ticket, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read ticket: %w", err)
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return fmt.Errorf("ticket id is required")
	}

	fmt.Print("Description: ")
	description, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read description: %w", err)
	}
	description = strings.TrimSpace(description)

	rf, err := reposFile()
	if err != nil {
		return err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return err
	}
	if len(cfg.Repos) == 0 {
		return fmt.Errorf("no repos defined in %s", rf)
	}

	selectedRepos, err := fzfSelectMulti(cfg.repoNames())
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

	var taskRepos []TaskRepo

	for _, repoName := range selectedRepos {
		repo := cfg.findRepo(repoName)
		if repo == nil {
			fmt.Fprintf(os.Stderr, "warning: repo %q not found in config, skipping\n", repoName)
			continue
		}

		patches, err := selectPatches(repoName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: patch selection for %s: %v\n", repoName, err)
		}

		worktreePath := filepath.Join(wtRoot, ticket, repo.Name)
		branch := ticket

		if err := runHooks("pre-create", ticket, repo.Name, worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: pre-create hooks for %s: %v\n", repo.Name, err)
		}

		if err := setupWorktree(repo, ticket, worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "error: setting up worktree for %s: %v\n", repo.Name, err)
			continue
		}

		for _, p := range patches {
			if err := gitApplyPatch(worktreePath, p); err != nil {
				fmt.Fprintf(os.Stderr, "warning: applying patch %s to %s: %v\n", p, repo.Name, err)
			}
		}

		if err := runHooks("post-create", ticket, repo.Name, worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: post-create hooks for %s: %v\n", repo.Name, err)
		}

		if _, err := os.Stat(filepath.Join(worktreePath, "package-lock.json")); err == nil {
			ensureNodeCache(worktreePath)
		}

		taskRepos = append(taskRepos, TaskRepo{
			Repo:         repo.Name,
			Branch:       branch,
			WorktreePath: worktreePath,
		})
	}

	if len(taskRepos) == 0 {
		return fmt.Errorf("no repos were successfully set up; not writing task state")
	}

	st := &TaskState{
		Ticket:      ticket,
		Description: description,
		CreatedAt:   time.Now().UTC(),
		Repos:       taskRepos,
	}

	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	if err := writeTaskState(tf, st); err != nil {
		return fmt.Errorf("write task state: %w", err)
	}

	fmt.Printf("Task %s created with %d repo(s); state written to %s\n", ticket, len(taskRepos), tf)
	return nil
}

// selectPatches globs <config dir>/patches/<repo>/*.patch and, if any
// exist, lets the user multiselect which to apply via fzf. If there are
// zero patches for the repo, the fzf call is skipped entirely and an empty
// slice is returned.
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
	return fzfSelectMulti(matches)
}

// setupWorktree ensures the repo's base bare clone exists (cloning or
// fetching as needed) and creates the worktree for ticket at worktreePath.
func setupWorktree(repo *RepoConfig, ticket, worktreePath string) error {
	base := expandHome(repo.Base)

	if _, err := os.Stat(base); os.IsNotExist(err) {
		if isSSHURL(repo.Origin) {
			checkSSHAgent()
		}
		if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(base), err)
		}
		if err := gitCloneBare(repo.Origin, base); err != nil {
			return fmt.Errorf("clone %s: %w", repo.Origin, err)
		}
	} else {
		if err := gitFetch(base); err != nil {
			return fmt.Errorf("fetch %s: %w", base, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(worktreePath), err)
	}

	if err := gitWorktreeAddNewBranch(base, worktreePath, ticket, repo.DefaultBranch); err != nil {
		// Branch may already exist from a prior `fleet-task new` run for
		// this ticket; fall back to adding a worktree for the existing
		// branch instead of treating this as fatal.
		fmt.Fprintf(os.Stderr, "warning: worktree add -b failed (%v), retrying without -b (branch may already exist)\n", err)
		if err2 := gitWorktreeAddExistingBranch(base, worktreePath, ticket); err2 != nil {
			return fmt.Errorf("worktree add (existing branch): %w", err2)
		}
	}

	return nil
}
