package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runTmux runs `tmux <args...>` with stdout/stderr inherited (so tmux's own
// error messages surface directly) and returns an error if it exits
// non-zero.
func runTmux(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// tmuxOutput runs `tmux <args...>` and returns its captured stdout.
func tmuxOutput(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return out.String(), err
}

// sessionExists reports whether the fleet tmux session already exists.
func sessionExists(session string) bool {
	cmd := exec.Command("tmux", hasSessionArgs(session)...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// ensureSession makes sure the single fixed fleet session exists, creating
// it (optionally with a custom tmux config file) if not. It never renames or
// recreates an existing session.
func ensureSession(session, configFile string) error {
	if sessionExists(session) {
		return nil
	}
	if configFile != "" {
		configFile = expandHome(configFile)
	}
	args := newSessionArgv(session, configFile)
	if err := runTmux(args...); err != nil {
		return fmt.Errorf("create tmux session %q: %w", session, err)
	}
	return nil
}

// listWindows returns the names of all windows currently in the session.
// Returns an empty slice (not an error) if the session doesn't exist yet.
func listWindows(session string) ([]string, error) {
	if !sessionExists(session) {
		return nil, nil
	}
	out, err := tmuxOutput(listWindowsArgs(session)...)
	if err != nil {
		return nil, fmt.Errorf("list windows in session %q: %w", session, err)
	}
	return splitNonEmptyLines(out), nil
}

// splitNonEmptyLines splits tmux's newline-delimited output, dropping any
// trailing blank line.
func splitNonEmptyLines(s string) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// wslToWindowsPath translates a WSL-side path to its win32 form via
// `wslpath -w`, for handing to powershell.exe (which can't resolve
// \\wsl.localhost paths as a working directory the same way, and tmux's -c
// only understands WSL-side paths anyway).
func wslToWindowsPath(dir string) (string, error) {
	out, err := exec.Command("wslpath", "-w", dir).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -w %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// windowsWorktreeWinPath resolves a runtime: windows repo's worktree dir to
// its win32 form. dir may be a symlink into /mnt/c/... (fleet-task creates
// one at the normal WSL worktree location for runtime: windows repos, whose
// real git worktree lives on an NTFS volume). wslpath does pure syntactic
// path translation and does NOT resolve symlinks, so translating the
// symlink's own (WSL ext4-side) path would map to a \\wsl.localhost\... UNC
// path instead of the correct C:\... location. Resolving it first is a
// no-op for any windows-runtime worktree that isn't a symlink.
func windowsWorktreeWinPath(dir string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return wslToWindowsPath(dir)
}

// newWindow creates a tmux window for the given pair under the given
// ticket's window name. For linux-runtime repos it runs the command
// directly in the worktree dir; for windows-runtime repos it translates the
// worktree dir to a win32 path and wraps the command in a powershell.exe
// invocation.
func newWindow(session, name string, p RunPair) error {
	if p.Runtime == "windows" {
		winDir, err := windowsWorktreeWinPath(p.WorktreeDir)
		if err != nil {
			return err
		}
		return runTmux(newWindowArgsWindows(session, name, winDir, p.Cmd)...)
	}
	return runTmux(newWindowArgsLinux(session, name, p.WorktreeDir, p.Cmd)...)
}

// killWindow kills a single named window in the session.
func killWindow(session, name string) error {
	return runTmux(killWindowArgs(session, name)...)
}
