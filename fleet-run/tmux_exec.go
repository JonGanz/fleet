package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// tmuxOutputQuiet is tmuxOutput with stderr discarded rather than inherited,
// for calls whose failure is an expected, silently-handled outcome (e.g.
// reading a session option that may simply not be set yet) -- callers that
// swallow the error shouldn't still leak tmux's raw error text to the
// terminal.
func tmuxOutputQuiet(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
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

// nextFreeWindowIndex returns the lowest window index in session that isn't
// already in use, so new-window can be given an explicit "=<session>:<index>"
// target rather than relying on tmux's own (client-attachment-relative)
// placement rules.
func nextFreeWindowIndex(session string) (int, error) {
	out, err := tmuxOutput(listWindowIndicesArgs(session)...)
	if err != nil {
		return 0, fmt.Errorf("list window indices in session %q: %w", session, err)
	}
	used := make(map[int]bool)
	for _, l := range splitNonEmptyLines(out) {
		i, err := strconv.Atoi(l)
		if err != nil {
			return 0, fmt.Errorf("parse window index %q in session %q: %w", l, session, err)
		}
		used[i] = true
	}
	for i := 0; ; i++ {
		if !used[i] {
			return i, nil
		}
	}
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
//
// If the session doesn't exist yet, this creates it together with this
// window in one atomic tmux call (rather than a content-less `new-session`
// followed by a separate `new-window`), so tmux never gets the chance to
// create its own implicit default "bash" window -- every window in the
// session is always a real, named app window, which is what lets
// `fleet-run stop`'s picker and its "nothing left running" detection work
// correctly.
func newWindow(session, configFile, name string, p RunPair) error {
	if !sessionExists(session) {
		if configFile != "" {
			configFile = expandHome(configFile)
		}
		if p.Runtime == "windows" {
			winDir, err := windowsWorktreeWinPath(p.WorktreeDir)
			if err != nil {
				return err
			}
			return runTmux(newSessionWithWindowArgsWindows(session, configFile, name, winDir, p.Cmd)...)
		}
		return runTmux(newSessionWithWindowArgsLinux(session, configFile, name, p.WorktreeDir, p.Cmd)...)
	}

	index, err := nextFreeWindowIndex(session)
	if err != nil {
		return err
	}
	if p.Runtime == "windows" {
		winDir, err := windowsWorktreeWinPath(p.WorktreeDir)
		if err != nil {
			return err
		}
		return runTmux(newWindowArgsWindows(session, name, winDir, p.Cmd, index)...)
	}
	return runTmux(newWindowArgsLinux(session, name, p.WorktreeDir, p.Cmd, index)...)
}

// killWindow kills a single named window in the session.
func killWindow(session, name string) error {
	return runTmux(killWindowArgs(session, name)...)
}

// setSessionOption sets the given tmux user option (global scope -- see
// setSessionOptionArgs for why).
func setSessionOption(name, value string) error {
	return runTmux(setSessionOptionArgs(name, value)...)
}

// unsetSessionOption clears a tmux user option. Unsetting an option that
// was never set is not an error.
func unsetSessionOption(name string) error {
	return runTmux(unsetSessionOptionArgs(name)...)
}

// getSessionOption reads a tmux user option's value. found is false if the
// option isn't set (tmux prints nothing for an unset user option) or no
// tmux server is currently running at all.
func getSessionOption(name string) (value string, found bool, err error) {
	out, err := tmuxOutputQuiet(showSessionOptionArgs(name)...)
	if err != nil {
		return "", false, nil
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", false, nil
	}
	return out, true, nil
}
