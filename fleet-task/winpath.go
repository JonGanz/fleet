package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// wslToWindowsPath translates a WSL-side path to its win32 form via
// `wslpath -w`. Duplicated from fleet-run's identical helper (fleet-run/
// tmux_exec.go) per this repo's no-shared-Go-code-between-modules design.
func wslToWindowsPath(dir string) (string, error) {
	out, err := exec.Command("wslpath", "-w", dir).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -w %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// psQuote wraps s in single quotes for embedding in a PowerShell -Command
// string, doubling any embedded single quotes per PowerShell's own escaping
// convention (`it''s` for a literal `it's`).
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// runWindowsCommand runs args as a native Windows process (e.g. git.exe,
// npm) with working directory winDir (a win32-form path), by shelling out to
// powershell.exe from WSL. Every arg is individually single-quoted, so this
// resolves against the *spawned PowerShell process's own Windows PATH* --
// it does not depend on WSL's own $PATH/interop.appendWindowsPath, only on
// powershell.exe itself being reachable (already an existing dependency via
// fleet-run's run-command support).
//
// The trailing `exit $LASTEXITCODE` is required: PowerShell does not
// reliably propagate a native command's exit code as its own process exit
// code otherwise, which would make failures here silently look like success
// to Go's exec.Cmd.Run().
func runWindowsCommand(winDir string, args ...string) error {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = psQuote(a)
	}
	psCmd := fmt.Sprintf("cd %s; & %s; exit $LASTEXITCODE", psQuote(winDir), strings.Join(quoted, " "))

	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", psCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("powershell (%v) in %s: %w", args, winDir, err)
	}
	return nil
}

// windowsPathExists reports whether winDir exists, checked via PowerShell's
// Test-Path (a Windows-native check, not a WSL os.Stat across /mnt/...).
func windowsPathExists(winDir string) (bool, error) {
	psCmd := fmt.Sprintf("if (Test-Path %s) { exit 0 } else { exit 1 }", psQuote(winDir))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", psCmd)
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("Test-Path %s: %w", winDir, err)
}

// windowsMkdirAll creates winDir (and any missing parents) via PowerShell's
// New-Item, mirroring os.MkdirAll but as a Windows-native operation.
func windowsMkdirAll(winDir string) error {
	psCmd := fmt.Sprintf("New-Item -ItemType Directory -Force -Path %s | Out-Null; exit $LASTEXITCODE", psQuote(winDir))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", psCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("New-Item -Force %s: %w", winDir, err)
	}
	return nil
}

// driveLetter extracts the leading drive letter (e.g. "C:") from a win32
// path, or "" if it doesn't look like a drive-letter path.
func driveLetter(winPath string) string {
	if len(winPath) >= 2 && winPath[1] == ':' {
		return strings.ToUpper(winPath[:2])
	}
	return ""
}

// checkSameDrive verifies every non-empty win32 path in paths resolves to
// the same drive letter, returning a clear error naming the mismatched pair
// if not. NTFS hardlinks (used to populate node_modules from the cache)
// fail across volumes, so windows_base/windows_worktree_root/
// windows_cache_root must all live on the same drive.
func checkSameDrive(labeled map[string]string) error {
	var firstLabel, firstDrive string
	for label, winPath := range labeled {
		if winPath == "" {
			continue
		}
		d := driveLetter(winPath)
		if firstDrive == "" {
			firstLabel, firstDrive = label, d
			continue
		}
		if d != firstDrive {
			return fmt.Errorf("%s (%s) and %s (%s) must be on the same drive for hardlinking to work", firstLabel, firstDrive, label, d)
		}
	}
	return nil
}
