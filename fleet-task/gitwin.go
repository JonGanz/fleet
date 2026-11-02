package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// windowsGit is the gitRunner for runtime: windows repos. The bare clone and
// git worktree live on the Windows-native side (under repo.WindowsBase /
// cfg.WindowsWorktreeRoot, configured in WSL-visible /mnt/... form) and are
// operated on by git.exe via powershell.exe, since WSL git writing through
// the 9P bridge into a Windows-mounted path is what this whole mechanism
// exists to avoid. The worktree path every other consumer (fleet-run, hooks,
// fleet-cache) expects -- <worktree_root>/<ticket>/<repo> -- is a WSL-side
// symlink into the real tree at <windows_worktree_root>/<ticket>/<repo>.
type windowsGit struct {
	cfg *ReposConfig
}

// realPath returns the WSL-visible location of the real Windows-native
// worktree for a WSL-side worktreePath of the form .../<ticket>/<repo> (the
// contract's worktree layout), under windows_worktree_root. It errors if
// windows_worktree_root is unset.
func (g windowsGit) realPath(worktreePath string) (string, error) {
	if g.cfg == nil || g.cfg.WindowsWorktreeRoot == "" {
		return "", fmt.Errorf("repos.yaml has no top-level windows_worktree_root (required for runtime: windows repos)")
	}
	repo := filepath.Base(worktreePath)
	ticket := filepath.Base(filepath.Dir(worktreePath))
	return filepath.Join(expandHome(g.cfg.WindowsWorktreeRoot), ticket, repo), nil
}

func (g windowsGit) Preflight(base, worktreePath string) error {
	real, err := g.realPath(worktreePath)
	if err != nil {
		return err
	}
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	winReal, err := wslToWindowsPath(real)
	if err != nil {
		return fmt.Errorf("resolving windows_worktree_root %s: %w", real, err)
	}
	return checkSameDrive(map[string]string{
		"windows_base":          winBase,
		"windows_worktree_root": winReal,
	})
}

func (windowsGit) BaseExists(base string) (bool, error) {
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return false, fmt.Errorf("resolving windows base %s: %w", base, err)
	}
	exists, err := windowsPathExists(winBase)
	if err != nil {
		return false, fmt.Errorf("checking windows base %s: %w", winBase, err)
	}
	return exists, nil
}

// CloneBare: unlike the Linux path, deliberately skips checkSSHAgent() --
// that checks WSL's own ssh-add, which has no bearing on Windows git.exe's
// own credential setup (Git for Windows / Windows OpenSSH manage that
// independently).
func (g windowsGit) CloneBare(origin, base string) error {
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	winParent, err := wslToWindowsPath(parentDir(base))
	if err != nil {
		return fmt.Errorf("resolving parent of windows_base: %w", err)
	}
	if err := windowsMkdirAll(winParent); err != nil {
		return fmt.Errorf("mkdir %s: %w", winParent, err)
	}
	if err := runWindowsCommand(winParent, "git", "clone", "--bare", origin, winBase); err != nil {
		return err
	}
	if err := runWindowsCommand(winBase, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return err
	}
	return g.Fetch(base)
}

func (windowsGit) Fetch(base string) error {
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	return runWindowsCommand(winBase, "git", "fetch", "origin")
}

func (windowsGit) Prune(base string) error {
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	return runWindowsCommand(winBase, "git", "worktree", "prune")
}

// WorktreeAdd creates the real worktree under windows_worktree_root via
// git.exe, then leaves worktreePath as a symlink to it -- only once the real
// worktree exists.
func (g windowsGit) WorktreeAdd(base, worktreePath, branch, defaultBranch string) error {
	real, err := g.realPath(worktreePath)
	if err != nil {
		return err
	}
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	winReal, err := wslToWindowsPath(real)
	if err != nil {
		return fmt.Errorf("resolving windows worktree %s: %w", real, err)
	}
	winRealParent, err := wslToWindowsPath(parentDir(real))
	if err != nil {
		return fmt.Errorf("resolving worktree parent: %w", err)
	}
	if err := windowsMkdirAll(winRealParent); err != nil {
		return fmt.Errorf("mkdir %s: %w", winRealParent, err)
	}

	if err := addWorktreeWithFallback(branch,
		func() error {
			return runWindowsCommandCapture(winBase, "git", "worktree", "add", winReal, "-b", branch, "origin/"+defaultBranch)
		},
		func() error {
			return runWindowsCommandCapture(winBase, "git", "worktree", "add", winReal, branch)
		}); err != nil {
		return err
	}

	return replaceSymlink(worktreePath, real)
}

// WorktreeRemove deregisters the real worktree (resolved via the WSL-side
// symlink) through git.exe, then removes the symlink itself. If we only
// removed the symlink, the real Windows-native tree and its worktree
// registration in windows_base would silently leak.
func (g windowsGit) WorktreeRemove(base, worktreePath string) error {
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base: %w", err)
	}

	real, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		// Symlink is already gone/broken (e.g. the real tree was deleted by
		// hand); nothing left to deregister directly, but clear any stale
		// registration and fall through to removing whatever's here.
		warnf("resolving symlink %s: %v", worktreePath, err)
		if perr := g.Prune(base); perr != nil {
			warnf("worktree prune for %s: %v", base, perr)
		}
	} else {
		winReal, err := wslToWindowsPath(real)
		if err != nil {
			return fmt.Errorf("resolving worktree path: %w", err)
		}
		if err := runWindowsCommand(winBase, "git", "worktree", "remove", winReal, "--force"); err != nil {
			return fmt.Errorf("git worktree remove: %w", err)
		}
	}

	if err := os.Remove(worktreePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing symlink %s: %w", worktreePath, err)
	}
	return nil
}

// WorktreeMove moves the real worktree under windows_worktree_root via
// git.exe, then recreates the WSL-side symlink at the new path (and drops
// the old one).
func (g windowsGit) WorktreeMove(base, oldPath, newPath string) error {
	oldReal, err := g.realPath(oldPath)
	if err != nil {
		return err
	}
	newReal, err := g.realPath(newPath)
	if err != nil {
		return err
	}
	winBase, err := wslToWindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base: %w", err)
	}
	winOldReal, err := wslToWindowsPath(oldReal)
	if err != nil {
		return fmt.Errorf("resolving old worktree: %w", err)
	}
	winNewReal, err := wslToWindowsPath(newReal)
	if err != nil {
		return fmt.Errorf("resolving new worktree: %w", err)
	}
	winNewParent, err := wslToWindowsPath(parentDir(newReal))
	if err != nil {
		return fmt.Errorf("resolving new worktree parent: %w", err)
	}
	if err := windowsMkdirAll(winNewParent); err != nil {
		return fmt.Errorf("mkdir %s: %w", winNewParent, err)
	}

	if err := runWindowsCommand(winBase, "git", "worktree", "move", winOldReal, winNewReal); err != nil {
		return fmt.Errorf("git worktree move: %w", err)
	}

	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing old symlink %s: %w", oldPath, err)
	}
	return replaceSymlink(newPath, newReal)
}

// ApplyPatch applies patchFile inside the real Windows-native worktree:
// WSL-native git run against the WSL-side symlink can't parse the Windows
// style gitdir: pointer in the worktree's .git file (it treats the
// drive-letter path as relative and fails with "not a git repository").
func (g windowsGit) ApplyPatch(worktreePath, patchFile string) error {
	real, err := g.realPath(worktreePath)
	if err != nil {
		return err
	}
	winReal, err := wslToWindowsPath(real)
	if err != nil {
		return fmt.Errorf("resolving worktree %s: %w", real, err)
	}
	winPatch, err := wslToWindowsPath(patchFile)
	if err != nil {
		return fmt.Errorf("resolving patch file %s: %w", patchFile, err)
	}
	return runWindowsCommand(winReal, "git", "apply", winPatch)
}

// replaceSymlink (re)creates link as a symlink to target, replacing whatever
// stale entry is there.
func replaceSymlink(link, target string) error {
	if err := os.MkdirAll(parentDir(link), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", parentDir(link), err)
	}
	if err := os.RemoveAll(link); err != nil {
		return fmt.Errorf("removing stale %s: %w", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", link, target, err)
	}
	return nil
}
