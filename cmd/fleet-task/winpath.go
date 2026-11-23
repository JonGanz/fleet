package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/proc"
	"github.com/JonGanz/fleet/internal/wsl"
)

// runWindowsCommand runs args as a native Windows process (e.g. git.exe,
// npm) with working directory winDir (a win32-form path), by shelling out to
// powershell.exe from WSL. See wsl.CommandScript.
func runWindowsCommand(winDir string, args ...string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", wsl.CommandScript(winDir, args...))
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
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", wsl.CommandScript(winDir, args...))
	var stderr bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("powershell (%v) in %s: %w%s", args, winDir, err, stderrSuffix(stderr.String()))
	}
	return nil
}

// runWindowsCommandTimeout is runWindowsCommand for network operations: the
// powershell.exe invocation is killed after timeout, with the same process
// group rule as runGitNetwork. Whether killing the WSL-side powershell.exe
// also ends the Windows-side git.exe is up to WSL interop.
func runWindowsCommandTimeout(winDir string, timeout time.Duration, args ...string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", wsl.CommandScript(winDir, args...))
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	err := proc.Run(cmd, proc.Options{Timeout: timeout, NewGroup: !cli.InteractiveAllowed()})
	if proc.IsTimeout(err) {
		return fmt.Errorf("powershell (%v) in %s: %w (set git.network_timeout in repos.yaml if the network is just slow)", args, winDir, err)
	}
	if err != nil {
		return fmt.Errorf("powershell (%v) in %s: %w", args, winDir, err)
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
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", wsl.CommandScript(winDir, args...))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("powershell (%v) in %s: %w%s", args, winDir, err, stderrSuffix(stderr.String()))
	}
	return stdout.String(), nil
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

// windowsDirIsEmptyScript builds the PowerShell script for windowsDirIsEmpty:
// exit 0 if winDir has no entries (hidden ones included), 3 if it has some,
// 1 on error (cmdlets don't set $LASTEXITCODE, so errors are caught).
func windowsDirIsEmptyScript(winDir string) string {
	return fmt.Sprintf(
		"$ErrorActionPreference='Stop'; try { if (@(Get-ChildItem -Force -LiteralPath %s).Count -eq 0) { exit 0 } else { exit 3 } } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }",
		wsl.PSQuote(winDir))
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

// windowsMkdirScript builds the PowerShell script for windowsMkdirAll. New-Item
// is a cmdlet, which does not set $LASTEXITCODE (so `exit $LASTEXITCODE`
// would exit 0 even when it fails); under $ErrorActionPreference='Stop' a
// failure throws and the catch block exits non-zero.
func windowsMkdirScript(winDir string) string {
	return fmt.Sprintf(
		"$ErrorActionPreference='Stop'; try { New-Item -ItemType Directory -Force -Path %s | Out-Null; exit 0 } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }",
		wsl.PSQuote(winDir))
}

// windowsPathExists reports whether winDir exists, checked via PowerShell's
// Test-Path (a Windows-native check, not a WSL os.Stat across /mnt/...).
func windowsPathExists(winDir string) (bool, error) {
	psCmd := fmt.Sprintf("if (Test-Path %s) { exit 0 } else { exit 1 }", wsl.PSQuote(winDir))
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

// windowsPathKey normalizes a win32 path for comparison: git.exe prints
// worktree paths with forward slashes, wslpath -w with backslashes, and
// NTFS paths are case-insensitive.
func windowsPathKey(p string) string {
	return strings.ToLower(strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(p), "/", `\`), `\`))
}
