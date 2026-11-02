package main

import (
	"fmt"
	"path/filepath"
)

// gitRunner abstracts "how git is driven" for one repo's runtime: linuxGit
// runs WSL-native git against WSL paths, windowsGit runs Windows-native
// git.exe (via powershell.exe) against win32 paths, with the real worktree
// on the NTFS side and a WSL-side symlink at the normal worktree path.
//
// Every path argument is in WSL-visible form (the convention used throughout
// repos.yaml and the state files); the windows implementation converts to
// win32 form itself. This keeps callers (new/edit/rm/mv) free of per-runtime
// switches.
type gitRunner interface {
	// Preflight validates a base/worktree pair before any work starts.
	Preflight(base, worktreePath string) error
	BaseExists(base string) (bool, error)
	// CloneBare clones origin as a bare repo at base (creating parent dirs)
	// and configures the remote-tracking fetch refspec.
	CloneBare(origin, base string) error
	Fetch(base string) error
	// Prune clears stale worktree registrations (`git worktree prune`).
	Prune(base string) error
	// WorktreeAdd creates a worktree at worktreePath on branch, branching
	// off origin/defaultBranch, or attaching branch if it already exists.
	WorktreeAdd(base, worktreePath, branch, defaultBranch string) error
	WorktreeRemove(base, worktreePath string) error
	WorktreeMove(base, oldPath, newPath string) error
	ApplyPatch(worktreePath, patchFile string) error
}

// gitFor returns the gitRunner for repo along with its base clone path in
// WSL-visible form. For linux repos base may be "" (repos.yaml has neither
// `base` nor defaults.base_root); callers decide whether that's fatal (new)
// or best-effort (rm, mv).
func gitFor(cfg *ReposConfig, repo *RepoConfig) (gitRunner, string, error) {
	if repo.Runtime == "windows" {
		if repo.WindowsBase == "" {
			return nil, "", fmt.Errorf("repo %s is runtime: windows but has no windows_base (set it directly or via defaults.windows_base_root)", repo.Name)
		}
		return windowsGit{cfg: cfg}, expandHome(repo.WindowsBase), nil
	}
	return linuxGit{}, expandHome(repo.Base), nil
}

// gitForExisting is gitFor for operating on an already-created worktree
// (rm, mv): if the repo is missing from repos.yaml, or has no usable base,
// it falls back to a linux runner whose "base" is the worktree itself --
// any worktree checkout has a .git file pointing back at its base repo, so
// `git -C <worktree> worktree remove|move` still works.
func gitForExisting(cfg *ReposConfig, repoName, worktreePath, what string) (gitRunner, string, error) {
	if repo := cfg.findRepo(repoName); repo != nil {
		r, base, err := gitFor(cfg, repo)
		if err != nil {
			return nil, "", err
		}
		if base != "" {
			return r, base, nil
		}
	}
	warnf("repo %q not found in repos.yaml (or has no base), trying %s without a base", repoName, what)
	return linuxGit{}, worktreePath, nil
}

// addWorktreeWithFallback runs addNew (create the branch), and on failure
// retries with addExisting (attach an existing branch, e.g. a prior run for
// this ticket). If both fail, the error reports both: a typo'd
// default_branch otherwise surfaces only as the second attempt's unrelated
// "invalid reference: <branch>".
func addWorktreeWithFallback(branch string, addNew, addExisting func() error) error {
	errNew := addNew()
	if errNew == nil {
		return nil
	}
	errExisting := addExisting()
	if errExisting == nil {
		return nil
	}
	return fmt.Errorf("worktree add failed both ways:\n  creating branch %s: %v\n  attaching existing branch %s: %v", branch, errNew, branch, errExisting)
}

// parentDir is filepath.Dir, named for readability at call sites.
func parentDir(p string) string { return filepath.Dir(p) }
