package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// wslToWindowsPath translates a WSL-side path to its win32 form via
// `wslpath -w`. Duplicated from fleet-task/fleet-run's identical helper per
// this repo's no-shared-Go-code-between-modules design.
func wslToWindowsPath(dir string) (string, error) {
	out, err := exec.Command("wslpath", "-w", dir).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -w %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// windowsToWSLPath translates a win32-form path to its WSL-visible form via
// `wslpath -u` (e.g. `C:\Users\jon\AppData\Local` -> `/mnt/c/Users/jon/AppData/Local`).
func windowsToWSLPath(winPath string) (string, error) {
	out, err := exec.Command("wslpath", "-u", winPath).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -u %s: %w", winPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// psQuote wraps s in single quotes for embedding in a PowerShell -Command
// string, doubling any embedded single quote per PowerShell's own escaping
// convention.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// windowsCommandScript builds the PowerShell -Command text that runs args as
// a native process in winDir. Failures must surface as a non-zero exit:
//   - a failing Set-Location (cd) aborts instead of running in whatever
//     directory powershell started in;
//   - a missing command (statement-terminating, not script-terminating) is
//     caught and exits 1 rather than falling through to `exit $LASTEXITCODE`
//     with $LASTEXITCODE unset;
//   - $ErrorActionPreference is relaxed before the native call so a native
//     program writing to stderr isn't turned into a terminating error.
func windowsCommandScript(winDir string, args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = psQuote(a)
	}
	return fmt.Sprintf("$ErrorActionPreference='Stop'; try { Set-Location -LiteralPath %s; $ErrorActionPreference='Continue'; & %s; exit $LASTEXITCODE } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }",
		psQuote(winDir), strings.Join(quoted, " "))
}

// windowsRemoveScript builds the PowerShell -Command text that recursively
// deletes winPath if it exists. A cmdlet failure (e.g. a locked file) must
// not report success: Remove-Item's errors are non-terminating by default and
// leave $LASTEXITCODE untouched, so ErrorAction Stop + try/catch is used.
func windowsRemoveScript(winPath string) string {
	return fmt.Sprintf("$ErrorActionPreference='Stop'; try { if (Test-Path -LiteralPath %s) { Remove-Item -LiteralPath %s -Recurse -Force } } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }; exit 0",
		psQuote(winPath), psQuote(winPath))
}

// runWindowsCommand runs args as a native Windows process (e.g. npm) with
// working directory winDir (a win32-form path), by shelling out to
// powershell.exe from WSL. See fleet-task/winpath.go's identical helper for
// the full rationale (PATH resolution happens inside the spawned
// PowerShell's own environment; `exit $LASTEXITCODE` is required for
// correct exit-code propagation). Output goes to stderr.
func runWindowsCommand(winDir string, args ...string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", windowsCommandScript(winDir, args))
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
// if not. NTFS hardlinks fail across volumes, so the Windows worktree and
// Windows cache root must live on the same drive.
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
