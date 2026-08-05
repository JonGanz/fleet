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
// string, doubling any embedded single quotes per PowerShell's own escaping
// convention (`it''s` for a literal `it's`).
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// runWindowsCommand runs args as a native Windows process (e.g. npm) with
// working directory winDir (a win32-form path), by shelling out to
// powershell.exe from WSL. See fleet-task/winpath.go's identical helper for
// the full rationale (PATH resolution happens inside the spawned
// PowerShell's own environment; `exit $LASTEXITCODE` is required for
// correct exit-code propagation).
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

// runWindowsRemoveAll deletes winPath (recursively, if it exists) via
// PowerShell's Remove-Item -- a bulk filesystem operation that must run as a
// native Windows process rather than Go's os.RemoveAll reaching across the
// WSL9P boundary.
func runWindowsRemoveAll(winPath string) error {
	psCmd := fmt.Sprintf("if (Test-Path %s) { Remove-Item -Recurse -Force %s }; exit $LASTEXITCODE", psQuote(winPath), psQuote(winPath))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", psCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Remove-Item -Recurse -Force %s: %w", winPath, err)
	}
	return nil
}

// windowsLocalAppData reads Windows' %LOCALAPPDATA% via cmd.exe, for the
// Windows-native cache root's auto-default.
func windowsLocalAppData() (string, error) {
	out, err := exec.Command("cmd.exe", "/C", "echo %LOCALAPPDATA%").Output()
	if err != nil {
		return "", fmt.Errorf("cmd.exe echo %%LOCALAPPDATA%%: %w", err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" || v == "%LOCALAPPDATA%" {
		return "", fmt.Errorf("%%LOCALAPPDATA%% not set")
	}
	return v, nil
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
