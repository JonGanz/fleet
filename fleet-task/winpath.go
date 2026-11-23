package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
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
// string, doubling any embedded single quote per PowerShell's own escaping
// convention.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// windowsCommandScript builds the PowerShell -Command script that runs args
// as a native Windows process with working directory winDir. Every arg is
// individually single-quoted, so this resolves against the *spawned
// PowerShell process's own Windows PATH* -- it does not depend on WSL's own
// $PATH/interop.appendWindowsPath.
//
// Failure handling: setting the directory runs under
// $ErrorActionPreference='Stop' inside try/catch, so a failed cd can't
// silently run the command in the wrong directory (it exits 1 instead). The
// preference is then relaxed to 'Continue' for the native command itself, so
// git's progress output on stderr isn't promoted to a terminating error.
// The trailing `exit $LASTEXITCODE` is required: PowerShell does not
// reliably propagate a native command's exit code as its own process exit
// code otherwise, which would make failures look like success to Go's
// exec.Cmd.Run().
func windowsCommandScript(winDir string, args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = psQuote(a)
	}
	return fmt.Sprintf(
		"$ErrorActionPreference='Stop'; try { Set-Location -LiteralPath %s; $ErrorActionPreference='Continue'; & %s; exit $LASTEXITCODE } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }",
		psQuote(winDir), strings.Join(quoted, " "))
}

// runWindowsCommand runs args as a native Windows process (e.g. git.exe,
// npm) with working directory winDir (a win32-form path), by shelling out to
// powershell.exe from WSL. See windowsCommandScript.
func runWindowsCommand(winDir string, args ...string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsCommandScript(winDir, args...))
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("powershell (%v) in %s: %w", args, winDir, err)
	}
	return nil
}

// runWindowsCommandCapture is runWindowsCommand with stderr captured into
// the returned error instead of shown live, for commands whose failure the
// caller reports itself (see addWorktree).
func runWindowsCommandCapture(winDir string, args ...string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsCommandScript(winDir, args...))
	var stderr bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("powershell (%v) in %s: %w%s", args, winDir, err, stderrSuffix(stderr.String()))
	}
	return nil
}

// windowsCommandOutput is windowsCommandOutputRaw with surrounding
// whitespace trimmed, for single-value output.
func windowsCommandOutput(winDir string, args ...string) (string, error) {
	s, err := windowsCommandOutputRaw(winDir, args...)
	return strings.TrimSpace(s), err
}

// windowsCommandOutputRaw is runWindowsCommand returning the command's
// stdout as is, with stderr folded into the error (which wraps the
// *exec.ExitError; see windowsExitCode).
func windowsCommandOutputRaw(winDir string, args ...string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsCommandScript(winDir, args...))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("powershell (%v) in %s: %w%s", args, winDir, err, stderrSuffix(stderr.String()))
	}
	return stdout.String(), nil
}

// windowsDirIsEmptyScript builds the PowerShell script for windowsDirIsEmpty:
// exit 0 if winDir has no entries (hidden ones included), 3 if it has some,
// 1 on error (cmdlets don't set $LASTEXITCODE, so errors are caught).
func windowsDirIsEmptyScript(winDir string) string {
	return fmt.Sprintf(
		"$ErrorActionPreference='Stop'; try { if (@(Get-ChildItem -Force -LiteralPath %s).Count -eq 0) { exit 0 } else { exit 3 } } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }",
		psQuote(winDir))
}

// windowsDirIsEmpty reports whether winDir has no entries, checked
// Windows-natively.
func windowsDirIsEmpty(winDir string) (bool, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsDirIsEmptyScript(winDir))
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if windowsExitCode(err) == 3 {
		return false, nil
	}
	return false, fmt.Errorf("listing %s: %w", winDir, err)
}

// windowsExitCode returns the exit code carried by err (from the helpers
// here), or -1 if powershell.exe didn't run to an exit status.
func windowsExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
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

// windowsMkdirScript builds the PowerShell script for windowsMkdirAll. New-Item
// is a cmdlet, which does not set $LASTEXITCODE (so `exit $LASTEXITCODE`
// would exit 0 even when it fails); under $ErrorActionPreference='Stop' a
// failure throws and the catch block exits non-zero.
func windowsMkdirScript(winDir string) string {
	return fmt.Sprintf(
		"$ErrorActionPreference='Stop'; try { New-Item -ItemType Directory -Force -Path %s | Out-Null; exit 0 } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }",
		psQuote(winDir))
}

// windowsMkdirAll creates winDir (and any missing parents) via PowerShell's
// New-Item, mirroring os.MkdirAll but as a Windows-native operation.
func windowsMkdirAll(winDir string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsMkdirScript(winDir))
	cmd.Stdout = out
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

// checkSameDrive verifies every non-empty win32 path in labeled resolves to
// the same drive letter, returning a clear error naming the mismatched pair
// if not. NTFS hardlinks (used to populate node_modules from the cache)
// fail across volumes, so windows_base/windows_worktree_root/
// windows_cache_root must all live on the same drive. Labels are checked in
// sorted order so the result (and message) is deterministic, and a path
// with no drive letter (UNC, or relative) is an error of its own rather
// than silently compared as "".
func checkSameDrive(labeled map[string]string) error {
	labels := make([]string, 0, len(labeled))
	for label, winPath := range labeled {
		if winPath != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)

	var firstLabel, firstDrive string
	for _, label := range labels {
		winPath := labeled[label]
		d := driveLetter(winPath)
		if d == "" {
			return fmt.Errorf("%s (%s) is not on a drive letter (UNC or relative path?); it must be on a local NTFS drive, the same one as the other Windows-side fleet paths, for hardlinking to work", label, winPath)
		}
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
