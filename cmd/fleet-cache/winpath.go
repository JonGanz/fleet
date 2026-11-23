package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/JonGanz/fleet/internal/wsl"
)

// windowsRemoveScript builds the PowerShell -Command text that recursively
// deletes winPath if it exists. A cmdlet failure (e.g. a locked file) must
// not report success: Remove-Item's errors are non-terminating by default and
// leave $LASTEXITCODE untouched, so ErrorAction Stop + try/catch is used.
func windowsRemoveScript(winPath string) string {
	return fmt.Sprintf("$ErrorActionPreference='Stop'; try { if (Test-Path -LiteralPath %s) { Remove-Item -LiteralPath %s -Recurse -Force } } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }; exit 0",
		wsl.PSQuote(winPath), wsl.PSQuote(winPath))
}

// runWindowsCommand runs args as a native Windows process (e.g. npm) with
// working directory winDir (a win32-form path), by shelling out to
// powershell.exe from WSL. See wsl.CommandScript for the full rationale
// (PATH resolution happens inside the spawned PowerShell's own environment;
// `exit $LASTEXITCODE` is required for correct exit-code propagation).
// Output goes to stderr. Cancelling ctx
// stops powershell.exe (see stopOnCancel).
func runWindowsCommand(ctx context.Context, winDir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", wsl.CommandScript(winDir, args...))
	stopOnCancel(cmd)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("powershell (%v) in %s: %w", args, winDir, err)
	}
	return nil
}

// runWindowsRemoveAll deletes winPath (recursively, if it exists) via
// PowerShell's Remove-Item -- a bulk filesystem operation that must run as a
// native Windows process rather than Go's os.RemoveAll reaching across the
// WSL9P boundary.
func runWindowsRemoveAll(winPath string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsRemoveScript(winPath))
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Remove-Item -Recurse -Force %s: %w", winPath, err)
	}
	return nil
}

// windowsLocalAppDataScript makes PowerShell print %LOCALAPPDATA% as UTF-8.
// cmd.exe's `echo` would emit the OEM codepage, mangling non-ASCII
// usernames when Go reads it as UTF-8.
const windowsLocalAppDataScript = "[Console]::OutputEncoding=[Text.Encoding]::UTF8; [Console]::Out.Write($env:LOCALAPPDATA)"

// parseLocalAppData cleans the output of windowsLocalAppDataScript.
func parseLocalAppData(out []byte) (string, error) {
	v := strings.TrimSpace(strings.TrimPrefix(string(out), "\ufeff"))
	if v == "" {
		return "", fmt.Errorf("%%LOCALAPPDATA%% not set")
	}
	return v, nil
}

// windowsLocalAppData reads Windows' %LOCALAPPDATA% via PowerShell, for the
// Windows-native cache root's auto-default.
func windowsLocalAppData() (string, error) {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsLocalAppDataScript).Output()
	if err != nil {
		return "", fmt.Errorf("powershell.exe reading %%LOCALAPPDATA%%: %w", err)
	}
	return parseLocalAppData(out)
}

// winJoin joins win32 path segments with a backslash, since Go's
// filepath.Join uses this binary's own OS separator ("/" when compiled for
// linux/WSL), which would silently produce a wrong path for a win32-form
// string.
func winJoin(base, sub string) string {
	return strings.TrimRight(base, `\`) + `\` + strings.TrimLeft(sub, `\`)
}
