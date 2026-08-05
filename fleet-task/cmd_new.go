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

	selectedRepos, err := selectMulti(cfg.repoNames())
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

	// Ask about every repo's patches up front, before any directory/worktree
	// work starts, so all user interaction for `new` front-loads instead of
	// interleaving prompts with (potentially slow) git/hook/cache work.
	patchesByRepo := make(map[string][]string, len(selectedRepos))
	for _, repoName := range selectedRepos {
		if cfg.findRepo(repoName) == nil {
			continue
		}
		patches, err := selectPatches(repoName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: patch selection for %s: %v\n", repoName, err)
			continue
		}
		patchesByRepo[repoName] = patches
	}

	var taskRepos []TaskRepo

	for _, repoName := range selectedRepos {
		repo := cfg.findRepo(repoName)
		if repo == nil {
			fmt.Fprintf(os.Stderr, "warning: repo %q not found in config, skipping\n", repoName)
			continue
		}

		patches := patchesByRepo[repoName]

		worktreePath := filepath.Join(wtRoot, ticket, repo.Name)
		branch := branchName(repo.BranchTemplate, ticket, description)

		if err := runHooks("pre-create", ticket, repo.Name, worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: pre-create hooks for %s: %v\n", repo.Name, err)
		}

		if err := setupWorktree(cfg, repo, ticket, branch, worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "error: setting up worktree for %s: %v\n", repo.Name, err)
			continue
		}

		for _, p := range patches {
			if err := applyPatch(cfg, repo, ticket, worktreePath, p); err != nil {
				fmt.Fprintf(os.Stderr, "warning: applying patch %s to %s: %v\n", p, repo.Name, err)
			}
		}

		if err := runHooks("post-create", ticket, repo.Name, worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: post-create hooks for %s: %v\n", repo.Name, err)
		}

		if _, err := os.Stat(filepath.Join(worktreePath, "package-lock.json")); err == nil {
			ensureNodeCache(repo, cfg, worktreePath)
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
// exist, lets the user multiselect which to apply, starting with every
// patch checked so the common case ("apply them all") needs no input —
// the user unchecks the ones they don't want. If there are zero patches
// for the repo, selection is skipped entirely and an empty slice is
// returned.
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
	return selectMultiPreselected(matches)
}

// applyPatch applies patchFile to worktreePath. For runtime: windows repos,
// worktreePath is a WSL-side symlink into a worktree created by native
// git.exe, whose .git file WSL-native git can't parse (see winGitApplyPatch),
// so it resolves the Windows-native worktree/patch paths and applies there
// instead of using worktreePath directly.
func applyPatch(cfg *ReposConfig, repo *RepoConfig, ticket, worktreePath, patchFile string) error {
	if repo.Runtime != "windows" {
		return gitApplyPatch(worktreePath, patchFile)
	}

	_, winWorktreeWin, err := windowsWorktreePaths(cfg, repo, ticket)
	if err != nil {
		return err
	}
	winPatchFile, err := wslToWindowsPath(patchFile)
	if err != nil {
		return fmt.Errorf("resolving patch file %s: %w", patchFile, err)
	}
	return winGitApplyPatch(winWorktreeWin, winPatchFile)
}

// windowsWorktreePaths computes the WSL-visible and win32-form paths for a
// runtime: windows repo's worktree, shared between setupWorktreeWindows and
// applyPatch so the formula only exists once.
func windowsWorktreePaths(cfg *ReposConfig, repo *RepoConfig, ticket string) (wslPath, winPath string, err error) {
	wslPath = filepath.Join(expandHome(cfg.WindowsWorktreeRoot), ticket, repo.Name)
	winPath, err = wslToWindowsPath(wslPath)
	if err != nil {
		return "", "", fmt.Errorf("resolving windows_worktree_root %s: %w", wslPath, err)
	}
	return wslPath, winPath, nil
}

// setupWorktree ensures the repo's base bare clone exists (cloning or
// fetching as needed) and creates the worktree for ticket at worktreePath on
// branch. For runtime: windows repos this delegates entirely to
// setupWorktreeWindows, since the actual git checkout must live on an NTFS
// volume, operated on by Windows-native git.exe.
func setupWorktree(cfg *ReposConfig, repo *RepoConfig, ticket, branch, worktreePath string) error {
	if repo.Runtime == "windows" {
		return setupWorktreeWindows(cfg, repo, ticket, branch, worktreePath)
	}

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

	if err := gitWorktreePrune(base); err != nil {
		fmt.Fprintf(os.Stderr, "warning: worktree prune for %s: %v\n", base, err)
	}

	if err := gitWorktreeAddNewBranch(base, worktreePath, branch, repo.DefaultBranch); err != nil {
		// Branch may already exist from a prior `fleet-task new` run for
		// this ticket; fall back to adding a worktree for the existing
		// branch instead of treating this as fatal.
		fmt.Fprintf(os.Stderr, "warning: worktree add -b failed (%v), retrying without -b (branch may already exist)\n", err)
		if err2 := gitWorktreeAddExistingBranch(base, worktreePath, branch); err2 != nil {
			return fmt.Errorf("worktree add (existing branch): %w", err2)
		}
	}

	return nil
}

// setupWorktreeWindows is the runtime: windows counterpart of setupWorktree:
// the bare clone and git worktree are created via Windows-native git.exe
// under repo.WindowsBase / cfg.WindowsWorktreeRoot (win32 paths, though
// configured in WSL-visible /mnt/... form), and worktreePath -- the normal
// WSL-side <worktree_root>/<ticket>/<repo> location every other consumer
// (fleet-run, hooks, fleet-cache) expects -- is left as a symlink into that
// location instead of a real directory.
func setupWorktreeWindows(cfg *ReposConfig, repo *RepoConfig, ticket, branch, worktreePath string) error {
	if repo.WindowsBase == "" {
		return fmt.Errorf("repo %s is runtime: windows but has no windows_base (set it directly or via defaults.windows_base_root)", repo.Name)
	}
	if cfg == nil || cfg.WindowsWorktreeRoot == "" {
		return fmt.Errorf("repo %s is runtime: windows but repos.yaml has no top-level windows_worktree_root", repo.Name)
	}

	winBaseWSL := expandHome(repo.WindowsBase)
	winBaseWin, err := wslToWindowsPath(winBaseWSL)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", winBaseWSL, err)
	}

	winWorktreeWSL, winWorktreeWin, err := windowsWorktreePaths(cfg, repo, ticket)
	if err != nil {
		return err
	}

	if err := checkSameDrive(map[string]string{
		"windows_base":          winBaseWin,
		"windows_worktree_root": winWorktreeWin,
	}); err != nil {
		return err
	}

	exists, err := windowsPathExists(winBaseWin)
	if err != nil {
		return fmt.Errorf("checking windows base %s: %w", winBaseWin, err)
	}
	if !exists {
		// Note: unlike the Linux path, we deliberately skip checkSSHAgent()
		// here -- that checks WSL's own ssh-add, which has no bearing on
		// Windows git.exe's own credential setup (Git for Windows / Windows
		// OpenSSH manage that independently).
		winParentDir, err := wslToWindowsPath(filepath.Dir(winBaseWSL))
		if err != nil {
			return fmt.Errorf("resolving parent of windows_base: %w", err)
		}
		if err := windowsMkdirAll(winParentDir); err != nil {
			return fmt.Errorf("mkdir %s: %w", winParentDir, err)
		}
		if err := winGitCloneBare(repo.Origin, winParentDir, winBaseWin); err != nil {
			return fmt.Errorf("clone %s: %w", repo.Origin, err)
		}
	} else {
		if err := winGitFetch(winBaseWin); err != nil {
			return fmt.Errorf("fetch %s: %w", winBaseWin, err)
		}
	}

	winWorktreeParentWin, err := wslToWindowsPath(filepath.Dir(winWorktreeWSL))
	if err != nil {
		return fmt.Errorf("resolving worktree parent: %w", err)
	}
	if err := windowsMkdirAll(winWorktreeParentWin); err != nil {
		return fmt.Errorf("mkdir %s: %w", winWorktreeParentWin, err)
	}

	if err := winGitWorktreePrune(winBaseWin); err != nil {
		fmt.Fprintf(os.Stderr, "warning: worktree prune for %s: %v\n", winBaseWin, err)
	}

	if err := winGitWorktreeAddNewBranch(winBaseWin, winWorktreeWin, branch, repo.DefaultBranch); err != nil {
		fmt.Fprintf(os.Stderr, "warning: worktree add -b failed (%v), retrying without -b (branch may already exist)\n", err)
		if err2 := winGitWorktreeAddExistingBranch(winBaseWin, winWorktreeWin, branch); err2 != nil {
			return fmt.Errorf("worktree add (existing branch): %w", err2)
		}
	}

	// Only now that the real Windows-native worktree exists, create the
	// WSL-side symlink every other consumer (fleet-run, hooks, fleet-cache)
	// expects at the normal worktreePath location.
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(worktreePath), err)
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		return fmt.Errorf("removing stale %s: %w", worktreePath, err)
	}
	if err := os.Symlink(winWorktreeWSL, worktreePath); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", worktreePath, winWorktreeWSL, err)
	}

	return nil
}
