package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/wsl"
	"github.com/JonGanz/fleet/internal/xdg"
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
	cfg *config.ReposConfig
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
	return filepath.Join(xdg.ExpandHome(g.cfg.WindowsWorktreeRoot), ticket, repo), nil
}

func (g windowsGit) Preflight(base, worktreePath string) error {
	real, err := g.realPath(worktreePath)
	if err != nil {
		return err
	}
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	winReal, err := wsl.WindowsPath(real)
	if err != nil {
		return fmt.Errorf("resolving windows_worktree_root %s: %w", real, err)
	}
	return wsl.CheckSameDrive(map[string]string{
		"windows_base":          winBase,
		"windows_worktree_root": winReal,
	})
}

// InspectBase mirrors linuxGit.InspectBase with Windows-native checks
// (Test-Path, git.exe, Get-ChildItem) instead of WSL os.Stat across /mnt.
func (windowsGit) InspectBase(base string) (baseInfo, error) {
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return baseInfo{}, fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	exists, err := windowsPathExists(winBase)
	if err != nil {
		return baseInfo{}, fmt.Errorf("checking windows base %s: %w", winBase, err)
	}
	if !exists {
		return baseInfo{}, nil
	}
	info := baseInfo{Exists: true}
	gitDir, err := windowsCommandOutput(winBase, "git", "rev-parse", "--git-dir")
	if err == nil && (gitDir == "." || gitDir == ".git") {
		info.IsRepo = true
	} else {
		if err != nil {
			info.Detail = err.Error()
		} else {
			info.Detail = "it is inside the repository at " + gitDir
		}
		empty, eerr := windowsDirIsEmpty(winBase)
		if eerr != nil {
			return baseInfo{}, eerr
		}
		info.Empty = empty
		return info, nil
	}
	url, err := windowsCommandOutput(winBase, "git", "config", "--get", "remote.origin.url")
	if err != nil && windowsExitCode(err) != 1 { // 1: key not set
		return baseInfo{}, err
	}
	info.OriginURL = url
	return info, nil
}

// InitBare: see linuxGit.InitBare for why init + remote add + fetch instead
// of `clone --bare`.
func (windowsGit) InitBare(base string) error {
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	winParent, err := wsl.WindowsPath(parentDir(base))
	if err != nil {
		return fmt.Errorf("resolving parent of windows_base: %w", err)
	}
	if err := windowsMkdirAll(winParent); err != nil {
		return fmt.Errorf("mkdir %s: %w", winParent, err)
	}
	return runWindowsCommand(winParent, "git", "init", "--bare", winBase)
}

func (windowsGit) AddOrigin(base, origin string) error {
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	return runWindowsCommand(winBase, "git", "remote", "add", "origin", origin)
}

// Fetch: unlike the Linux path, deliberately skips checkSSHAgent() -- that
// checks WSL's own ssh-add, which has no bearing on Windows git.exe's own
// credential setup (Git for Windows / Windows OpenSSH manage that
// independently).
func (windowsGit) Fetch(base, origin string) error {
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	return runWindowsCommand(winBase, "git", "fetch", "--prune", "origin")
}

func (windowsGit) Prune(base string) error {
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	return runWindowsCommand(winBase, "git", "worktree", "prune")
}

// RefExists runs `git show-ref --verify --quiet <ref>` via git.exe: exit 1
// means absent. (The script wrapper also exits 1 if it can't cd into base,
// but then the worktree add that follows fails with a clear error anyway.)
func (windowsGit) RefExists(base, ref string) (bool, error) {
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return false, fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	_, err = windowsCommandOutput(winBase, "git", "show-ref", "--verify", "--quiet", ref)
	if err == nil {
		return true, nil
	}
	if windowsExitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

// WorktreeAdd creates the real worktree under windows_worktree_root via
// git.exe, then leaves worktreePath as a symlink to it -- only once the real
// worktree exists.
func (g windowsGit) WorktreeAdd(base, worktreePath string, plan worktreeAddPlan) error {
	real, err := g.realPath(worktreePath)
	if err != nil {
		return err
	}
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base %s: %w", base, err)
	}
	winReal, err := wsl.WindowsPath(real)
	if err != nil {
		return fmt.Errorf("resolving windows worktree %s: %w", real, err)
	}
	winRealParent, err := wsl.WindowsPath(parentDir(real))
	if err != nil {
		return fmt.Errorf("resolving worktree parent: %w", err)
	}
	if err := windowsMkdirAll(winRealParent); err != nil {
		return fmt.Errorf("mkdir %s: %w", winRealParent, err)
	}

	if err := runWindowsCommandCapture(winBase, append([]string{"git"}, plan.args(winReal)...)...); err != nil {
		return err
	}

	return replaceSymlink(worktreePath, real)
}

// WorktreeRemove deregisters the real worktree (resolved via the WSL-side
// symlink) through git.exe, then removes the symlink itself. If we only
// removed the symlink, the real Windows-native tree and its worktree
// registration in windows_base would silently leak.
func (g windowsGit) WorktreeRemove(base, worktreePath string) error {
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base: %w", err)
	}

	real, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		// Symlink is already gone/broken (e.g. the real tree was deleted by
		// hand); nothing left to deregister directly, but clear any stale
		// registration and fall through to removing whatever's here.
		cli.Warnf("resolving symlink %s: %v", worktreePath, err)
		if perr := g.Prune(base); perr != nil {
			cli.Warnf("worktree prune for %s: %v", base, perr)
		}
	} else {
		winReal, err := wsl.WindowsPath(real)
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
	winBase, err := wsl.WindowsPath(base)
	if err != nil {
		return fmt.Errorf("resolving windows_base: %w", err)
	}
	winOldReal, err := wsl.WindowsPath(oldReal)
	if err != nil {
		return fmt.Errorf("resolving old worktree: %w", err)
	}
	winNewReal, err := wsl.WindowsPath(newReal)
	if err != nil {
		return fmt.Errorf("resolving new worktree: %w", err)
	}
	winNewParent, err := wsl.WindowsPath(parentDir(newReal))
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
	winReal, err := wsl.WindowsPath(real)
	if err != nil {
		return fmt.Errorf("resolving worktree %s: %w", real, err)
	}
	winPatch, err := wsl.WindowsPath(patchFile)
	if err != nil {
		return fmt.Errorf("resolving patch file %s: %w", patchFile, err)
	}
	return runWindowsCommand(winReal, "git", "apply", winPatch)
}

// replaceSymlink (re)creates link as a symlink to target. Only a symlink (or
// nothing) at link is replaced: a real file or directory there may be
// someone's work, so it is an error rather than something to delete.
func replaceSymlink(link, target string) error {
	if err := os.MkdirAll(parentDir(link), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", parentDir(link), err)
	}
	fi, err := os.Lstat(link)
	switch {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		if err := os.Remove(link); err != nil {
			return fmt.Errorf("removing stale symlink %s: %w", link, err)
		}
	case err == nil:
		return fmt.Errorf("%s already exists and is not a symlink; move it away and retry", link)
	case !os.IsNotExist(err):
		return fmt.Errorf("checking %s: %w", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", link, target, err)
	}
	return nil
}

// Changes is the approximate windows-runtime check: `git status
// --porcelain` via git.exe, minus every file a recorded patch touches. The
// exact temp-index method (see linuxGit.Changes) would need GIT_INDEX_FILE
// juggling and temp files on the Windows side through powershell.exe, so
// this trades exactness for simplicity: an edit inside a patched file is
// not seen (hence Approximate).
func (g windowsGit) Changes(worktreePath string, patchFiles []string, exclude bool) changeReport {
	real, err := g.realPath(worktreePath)
	if err != nil {
		return unknownChanges(err.Error())
	}
	winReal, err := wsl.WindowsPath(real)
	if err != nil {
		return unknownChanges(fmt.Sprintf("resolving worktree %s: %v", real, err))
	}
	status, err := windowsCommandOutputRaw(winReal, "git", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return unknownChanges(err.Error())
	}
	files := parsePorcelainLines(status)
	if !exclude {
		return reportFromFiles(files, false, false, "")
	}
	if len(patchFiles) == 0 {
		return reportFromFiles(files, true, false, "")
	}
	kept, ok := subtractPatchedFiles(files, patchFiles)
	if !ok {
		return reportFromFiles(files, false, false, "a recorded patch file could not be read, so patch changes are included")
	}
	return reportFromFiles(kept, true, true, "")
}

// UnpushedCommits runs `git rev-list --count refs/heads/<branch> --not
// --remotes` via git.exe in the real worktree.
func (g windowsGit) UnpushedCommits(worktreePath, branch string) (int, error) {
	real, err := g.realPath(worktreePath)
	if err != nil {
		return 0, err
	}
	winReal, err := wsl.WindowsPath(real)
	if err != nil {
		return 0, err
	}
	s, err := windowsCommandOutput(winReal, "git", "rev-list", "--count", "refs/heads/"+branch, "--not", "--remotes")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
