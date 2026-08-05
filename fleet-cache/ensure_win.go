package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed hardlink_windows.ps1
var hardlinkWindowsScript []byte

// runEnsureWindows implements `fleet-cache ensure-windows <wsl-worktree-dir>
// [--cache-root <wsl-cache-root>]`, the runtime: windows counterpart of
// runEnsure. The cache entry (npm ci) and the target node_modules tree are
// populated entirely via native Windows processes (PowerShell/npm), since
// hardlinks can't cross the WSL9P boundary and bulk file operations across
// it are slow; only single-file operations here (hashing, copying
// package.json/package-lock.json in) stay as plain Go os.* calls, per the
// project's pragmatic scope decision.
func runEnsureWindows(wslWorktreeDir, cacheRootFlag string) error {
	lockPath := filepath.Join(wslWorktreeDir, "package-lock.json")
	if _, err := os.Stat(lockPath); err != nil {
		return fmt.Errorf("no package-lock.json in %s: %w", wslWorktreeDir, err)
	}

	hash, err := hashFile(lockPath)
	if err != nil {
		return fmt.Errorf("hashing %s: %w", lockPath, err)
	}

	cacheRootWSL, err := resolveWindowsCacheRoot(cacheRootFlag)
	if err != nil {
		return fmt.Errorf("resolving windows cache root: %w", err)
	}

	targetWin, err := wslToWindowsPath(wslWorktreeDir)
	if err != nil {
		return fmt.Errorf("resolving worktree path: %w", err)
	}
	entryWSL := filepath.Join(cacheRootWSL, "node-cache", hash)
	entryWin, err := wslToWindowsPath(entryWSL)
	if err != nil {
		return fmt.Errorf("resolving cache entry dir: %w", err)
	}

	if err := checkSameDrive(map[string]string{
		"worktree (windows_worktree_root)": targetWin,
		"cache entry (windows_cache_root)": entryWin,
	}); err != nil {
		return err
	}

	entryNodeModulesWSL := filepath.Join(entryWSL, "node_modules")
	if _, err := os.Stat(entryNodeModulesWSL); os.IsNotExist(err) {
		if err := populateCacheEntryWindows(entryWSL, entryWin, lockPath); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("checking cache entry %s: %w", entryWSL, err)
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hardlinking node_modules into %s: %w", targetWin, err)
	}

	shortHash := hash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	fmt.Printf("linked node_modules from windows cache %s\n", shortHash)
	return nil
}

// populateCacheEntryWindows creates the cache entry dir, copies
// package-lock.json/package.json in (single-file Go ops, pragmatic), and
// runs `npm ci` as a native Windows process so node_modules gets populated
// under the Windows-native cache entry.
func populateCacheEntryWindows(entryWSL, entryWin, lockPath string) error {
	if err := os.MkdirAll(entryWSL, 0o755); err != nil {
		return fmt.Errorf("creating cache entry dir %s: %w", entryWSL, err)
	}

	if err := copyFile(lockPath, filepath.Join(entryWSL, "package-lock.json")); err != nil {
		return fmt.Errorf("copying package-lock.json into cache: %w", err)
	}
	pkgPath := filepath.Join(filepath.Dir(lockPath), "package.json")
	if err := copyFile(pkgPath, filepath.Join(entryWSL, "package.json")); err != nil {
		return fmt.Errorf("copying package.json into cache: %w", err)
	}

	if err := runWindowsCommand(entryWin, "npm", "ci"); err != nil {
		// Best-effort cleanup so a failed install doesn't leave a bad cache
		// entry that future ensure-windows calls treat as complete. npm ci
		// can leave many thousands of partial files, so this cleanup itself
		// runs as a native Windows bulk delete, not os.RemoveAll.
		_ = runWindowsRemoveAll(entryWin)
		return fmt.Errorf("npm ci failed in %s: %w", entryWin, err)
	}

	if _, err := os.Stat(filepath.Join(entryWSL, "node_modules")); err != nil {
		_ = runWindowsRemoveAll(entryWin)
		return fmt.Errorf("npm ci completed but node_modules missing in %s: %w", entryWin, err)
	}

	return nil
}

// resolveWindowsCacheRoot returns the Windows-native cache root in WSL-
// visible form: cacheRootFlag if set (already expanded/WSL-form, as passed
// through from repos.yaml's windows_cache_root by fleet-task), else
// %LOCALAPPDATA%\fleet auto-detected via cmd.exe, mirroring the Linux
// cacheRoot() default of ~/.cache/fleet.
func resolveWindowsCacheRoot(cacheRootFlag string) (string, error) {
	if cacheRootFlag != "" {
		return cacheRootFlag, nil
	}
	localAppData, err := windowsLocalAppData()
	if err != nil {
		return "", err
	}
	winRoot := winJoin(localAppData, "fleet")
	return windowsToWSLPath(winRoot)
}

// writeHardlinkScript writes the embedded hardlink_windows.ps1 content to
// <cacheRootWSL>/hardlink.ps1 (a single small file write, pragmatic) and
// returns its win32-form path. Overwritten on every call since it's tiny and
// static -- no versioning/staleness concern.
func writeHardlinkScript(cacheRootWSL string) (string, error) {
	if err := os.MkdirAll(cacheRootWSL, 0o755); err != nil {
		return "", fmt.Errorf("creating cache root %s: %w", cacheRootWSL, err)
	}
	scriptWSL := filepath.Join(cacheRootWSL, "hardlink.ps1")
	if err := os.WriteFile(scriptWSL, hardlinkWindowsScript, 0o644); err != nil {
		return "", fmt.Errorf("writing hardlink script %s: %w", scriptWSL, err)
	}
	return wslToWindowsPath(scriptWSL)
}
