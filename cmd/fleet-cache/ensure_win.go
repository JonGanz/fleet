package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/wsl"
)

//go:embed hardlink_windows.ps1
var hardlinkWindowsScript []byte

// resolveWindowsCacheRoot returns the Windows-native cache root in WSL-
// visible form: cacheRootFlag if set (already expanded/WSL-form, as passed
// through from repos.yaml's windows_cache_root by fleet-task), else
// %LOCALAPPDATA%\fleet auto-detected via PowerShell, mirroring the Linux
// xdg.CacheDir default of ~/.cache/fleet.
func resolveWindowsCacheRoot(cacheRootFlag string) (string, error) {
	if cacheRootFlag != "" {
		return cacheRootFlag, nil
	}
	localAppData, err := windowsLocalAppData()
	if err != nil {
		return "", err
	}
	winRoot := winJoin(localAppData, "fleet")
	return wsl.WSLPath(winRoot)
}

// runEnsureWindows implements `fleet-cache ensure-windows <wsl-worktree-dir>
// [--force] [--cache-root <wsl-cache-root>]`, the runtime: windows
// counterpart of runEnsure. The cache entry (npm ci) and the target
// node_modules tree are populated entirely via native Windows processes
// (PowerShell/npm), since hardlinks can't cross the WSL9P boundary and bulk
// file operations across it are slow; only single-file operations here
// (keying, copying package.json/package-lock.json/.npmrc in, the skip marker)
// stay as plain Go os.* calls, per the project's pragmatic scope decision.
//
// Like ensure, it skips (leaving node_modules -- and any `npm link`s --
// untouched) when the target's .fleet-cache-hash marker already matches
// the cache key (lockfile + .npmrc), unless force is set.
func runEnsureWindows(wslWorktreeDir string, force bool, cacheRootFlag string) error {
	lockPath, key, err := cacheKey(wslWorktreeDir)
	if err != nil {
		return err
	}

	targetNodeModulesWSL := filepath.Join(wslWorktreeDir, "node_modules")
	if !force && alreadyLinked(targetNodeModulesWSL, key) {
		logf("node_modules already linked to windows cache %s, nothing to do", shortHash(key))
		return nil
	}

	cacheRootWSL, err := resolveWindowsCacheRoot(cacheRootFlag)
	if err != nil {
		return fmt.Errorf("resolving windows cache root: %w", err)
	}

	targetWin, err := wsl.WindowsPath(wslWorktreeDir)
	if err != nil {
		return fmt.Errorf("resolving worktree path: %w", err)
	}
	entryWSL := filepath.Join(nodeCacheDir(cacheRootWSL), key)
	entryWin, err := wsl.WindowsPath(entryWSL)
	if err != nil {
		return fmt.Errorf("resolving cache entry dir: %w", err)
	}

	if err := wsl.CheckSameDrive(map[string]string{
		"worktree (windows_worktree_root)": targetWin,
		"cache entry (windows_cache_root)": entryWin,
	}); err != nil {
		return err
	}

	removeWin := func(wslPath string) error {
		winPath, err := wsl.WindowsPath(wslPath)
		if err != nil {
			return err
		}
		return runWindowsRemoveAll(winPath)
	}
	ctx, stopInterrupts := interruptContext()
	err = ensureCacheEntry(ctx, entryWSL, key, lockPath, entryOps{
		install: func(ctx context.Context, staging string) error {
			stagingWin, err := wsl.WindowsPath(staging)
			if err != nil {
				return err
			}
			if err := runWindowsCommand(ctx, stagingWin, "npm", "ci"); err != nil {
				return fmt.Errorf("npm ci failed in %s: %w", stagingWin, err)
			}
			return nil
		},
		remove: removeWin,
	})
	stopInterrupts()
	if err != nil {
		return err
	}

	targetNodeModulesWin := winJoin(targetWin, "node_modules")
	if err := runWindowsRemoveAll(targetNodeModulesWin); err != nil {
		return fmt.Errorf("removing existing node_modules in %s: %w", targetWin, err)
	}

	scriptWin, err := writeHardlinkScript(cacheRootWSL)
	if err != nil {
		return err
	}

	entryNodeModulesWin := winJoin(entryWin, "node_modules")
	psArgs := []string{
		"-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", scriptWin,
		"-Src", entryNodeModulesWin,
		"-Dst", targetNodeModulesWin,
	}
	cmd := exec.Command("powershell.exe", psArgs...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hardlinking node_modules into %s: %w", targetWin, err)
	}

	if err := writeMarker(targetNodeModulesWSL, key); err != nil {
		cli.Warnf("writing cache marker: %v", err)
	}

	logf("linked node_modules from windows cache %s", shortHash(key))
	return nil
}

// writeHardlinkScript writes the embedded hardlink_windows.ps1 content to
// <cacheRootWSL>/hardlink.ps1 (a single small file write, pragmatic) and
// returns its win32-form path. The file is only rewritten when its content
// differs, and replaced atomically (temp file + rename) so a concurrent
// ensure-windows never runs a half-written script.
func writeHardlinkScript(cacheRootWSL string) (string, error) {
	if err := os.MkdirAll(cacheRootWSL, 0o755); err != nil {
		return "", fmt.Errorf("creating cache root %s: %w", cacheRootWSL, err)
	}
	scriptWSL := filepath.Join(cacheRootWSL, "hardlink.ps1")
	if existing, err := os.ReadFile(scriptWSL); err != nil || !bytes.Equal(existing, hardlinkWindowsScript) {
		tmp := fmt.Sprintf("%s.tmp-%d", scriptWSL, os.Getpid())
		if err := os.WriteFile(tmp, hardlinkWindowsScript, 0o644); err != nil {
			return "", fmt.Errorf("writing hardlink script %s: %w", scriptWSL, err)
		}
		if err := os.Rename(tmp, scriptWSL); err != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("writing hardlink script %s: %w", scriptWSL, err)
		}
	}
	return wsl.WindowsPath(scriptWSL)
}
