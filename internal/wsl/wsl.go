// Package wsl holds the WSL2/Windows helpers shared by the fleet binaries
// for runtime: windows repos: translating paths between their WSL and win32
// forms with wslpath, quoting for and scripting PowerShell -Command
// strings, and the same-drive check NTFS hardlinks need. Running the
// resulting PowerShell commands stays with each binary, since where their
// output goes differs per tool.
package wsl

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// WindowsPath translates a WSL-side path to its win32 form via
// `wslpath -w` (e.g. /mnt/c/x -> C:\x). It does not resolve symlinks
// itself on every WSL version; callers that may hold a symlink resolve it
// first.
func WindowsPath(dir string) (string, error) {
	out, err := exec.Command("wslpath", "-w", dir).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -w %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WSLPath translates a win32-form path to its WSL-visible form via
// `wslpath -u` (e.g. `C:\Users\jon\AppData\Local` -> `/mnt/c/Users/jon/AppData/Local`).
func WSLPath(winPath string) (string, error) {
	out, err := exec.Command("wslpath", "-u", winPath).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -u %s: %w", winPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// PSQuote returns s as a PowerShell single-quoted (verbatim) string literal
// for embedding in a -Command string: the only character special inside one
// is the single quote itself, which is escaped by doubling it.
func PSQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// CommandScript builds the PowerShell -Command script that runs args as a
// native Windows process (e.g. git.exe, npm) with working directory winDir
// (a win32-form path). Every arg is individually single-quoted, so the
// command resolves against the spawned PowerShell process's own Windows
// PATH, not WSL's $PATH/interop.appendWindowsPath.
//
// Failures must surface as a non-zero exit:
//   - setting the directory runs under $ErrorActionPreference='Stop' inside
//     try/catch, so a failed cd can't silently run the command in whatever
//     directory PowerShell started in (it exits 1 instead);
//   - a missing command (statement-terminating, not script-terminating) is
//     caught and exits 1 rather than falling through to
//     `exit $LASTEXITCODE` with $LASTEXITCODE unset;
//   - the preference is relaxed to 'Continue' before the native call, so a
//     program writing progress to stderr (git does) isn't turned into a
//     terminating error;
//   - the trailing `exit $LASTEXITCODE` is required: PowerShell does not
//     reliably propagate a native command's exit code as its own otherwise,
//     which would make failures look like success to exec.Cmd.Run.
func CommandScript(winDir string, args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = PSQuote(a)
	}
	return fmt.Sprintf(
		"$ErrorActionPreference='Stop'; try { Set-Location -LiteralPath %s; $ErrorActionPreference='Continue'; & %s; exit $LASTEXITCODE } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }",
		PSQuote(winDir), strings.Join(quoted, " "))
}

// DriveLetter extracts the leading drive letter, upper-cased (e.g. "C:"),
// from a win32 path, or "" if it doesn't look like a drive-letter path.
func DriveLetter(winPath string) string {
	if len(winPath) >= 2 && winPath[1] == ':' {
		return strings.ToUpper(winPath[:2])
	}
	return ""
}

// CheckSameDrive verifies every non-empty win32 path in labeled (label ->
// path) is on the same drive letter, returning an error naming the
// mismatched pair if not. NTFS hardlinks (used to populate node_modules
// from the Windows-native cache) fail across volumes, so windows_base,
// windows_worktree_root and the Windows cache root must all live on one
// drive. Labels are checked in sorted order so the outcome (and message)
// never depends on map iteration. A path with no drive letter (a
// \\wsl.localhost\... UNC path, i.e. something inside the WSL filesystem,
// or a relative path) is an error of its own rather than compared as "":
// it is not on an NTFS volume at all.
func CheckSameDrive(labeled map[string]string) error {
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
		d := DriveLetter(winPath)
		if d == "" {
			return fmt.Errorf("%s (%s) is not on a Windows drive (no drive letter; a UNC or relative path?); it must be on a local NTFS drive such as C:, the same one as the other Windows-side fleet paths, for hardlinking to work", label, winPath)
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
