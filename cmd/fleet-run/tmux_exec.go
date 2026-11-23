package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JonGanz/fleet/internal/tmuxsock"
	"github.com/JonGanz/fleet/internal/wsl"
	"github.com/JonGanz/fleet/internal/xdg"
)

// tmuxWindow is one window of the fleet session.
type tmuxWindow struct {
	ID   string // server-unique "@N"; the only safe way to target it
	Name string
}

// runTmux runs `tmux <args...>` with its output sent to stderr (so tmux's own
// error messages surface directly, while stdout stays reserved for results
// such as --json output) and returns an error if it exits non-zero.
func runTmux(args ...string) error {
	cmd := tmuxCommand(args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// tmuxCommand builds an exec.Cmd for tmux, honouring FLEET_TMUX_SOCKET. Every
// tmux invocation must go through here.
func tmuxCommand(args ...string) *exec.Cmd {
	return exec.Command("tmux", tmuxsock.Args(args...)...)
}

// tmuxOutput runs `tmux <args...>` and returns its captured stdout.
func tmuxOutput(args ...string) (string, error) {
	cmd := tmuxCommand(args...)
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
	cmd := tmuxCommand(args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// sessionExists reports whether the fleet tmux session already exists.
func sessionExists(session string) bool {
	return tmuxCommand(hasSessionArgs(session)...).Run() == nil
}

// listWindows returns the windows currently in the session. Returns an
// empty slice (not an error) if the session doesn't exist yet.
func listWindows(session string) ([]tmuxWindow, error) {
	if !sessionExists(session) {
		return nil, nil
	}
	out, err := tmuxOutput(listWindowsArgs(session)...)
	if err != nil {
		return nil, fmt.Errorf("list windows in session %q: %w", session, err)
	}
	return parseWindowLines(out)
}

// parseWindowLines parses listWindowsArgs output ("@N<tab>name" lines).
func parseWindowLines(out string) ([]tmuxWindow, error) {
	var windows []tmuxWindow
	for _, line := range splitNonEmptyLines(out) {
		id, name, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(id, "@") {
			return nil, fmt.Errorf("unexpected tmux window line %q", line)
		}
		windows = append(windows, tmuxWindow{ID: id, Name: name})
	}
	return windows, nil
}

// windowNames returns the names of windows, in order.
func windowNames(windows []tmuxWindow) []string {
	names := make([]string, len(windows))
	for i, w := range windows {
		names[i] = w.Name
	}
	return names
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

// windowsWorktreeWinPath resolves a runtime: windows repo's worktree dir to
// its win32 form. dir may be a symlink into /mnt/c/... (fleet-task creates
// one at the normal WSL worktree location for runtime: windows repos, whose
// real git worktree lives on an NTFS volume). wslpath may do pure
// syntactic translation without resolving symlinks (it depends on the WSL
// version), in which case the symlink's own (WSL ext4-side) path would map to
// a \\wsl.localhost\... UNC path instead of the correct C:\... location.
// Resolving it first is a no-op for any windows-runtime worktree that isn't a symlink.
func windowsWorktreeWinPath(dir string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return wsl.WindowsPath(dir)
}

// checkWorktreeDir verifies the worktree directory exists (following
// symlinks, so a dangling windows-runtime worktree symlink is caught too).
// Without this tmux silently falls back to $HOME when -c points at a missing
// dir, and the app would run in the wrong place while reporting success.
// For windows-runtime repos this checks the WSL-side path, before any
// wslpath translation.
func checkWorktreeDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("worktree dir %s is not accessible (stale task state? try fleet-task rm): %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("worktree path %s is not a directory", dir)
	}
	return nil
}

// newWindow creates a tmux window for the given pair. For linux-runtime
// repos it runs the command directly in the worktree dir; for
// windows-runtime repos it translates the worktree dir to a win32 path and
// wraps the command in a powershell.exe invocation. It fails (creating
// nothing) if the worktree dir doesn't exist.
//
// If the session doesn't exist yet, this creates it together with this
// window in one atomic tmux call (rather than a content-less `new-session`
// followed by a separate `new-window`), so tmux never gets the chance to
// create its own implicit default "bash" window -- every window in the
// session is always a real, named app window, which is what lets
// `fleet-run stop`'s picker and its "nothing left running" detection work
// correctly.
func newWindow(session, configFile, name string, p RunPair) error {
	winDir, err := pairLaunchDir(p)
	if err != nil {
		return err
	}
	isWindows := p.Runtime == "windows"

	if !sessionExists(session) {
		if configFile != "" {
			configFile = xdg.ExpandHome(configFile)
		}
		if isWindows {
			return runTmux(newSessionWithWindowArgsWindows(session, configFile, name, winDir, p.Cmd)...)
		}
		return runTmux(newSessionWithWindowArgsLinux(session, configFile, name, p.WorktreeDir, p.Cmd)...)
	}

	index, err := nextFreeWindowIndex(session)
	if err != nil {
		return err
	}
	if isWindows {
		return runTmux(newWindowArgsWindows(session, name, winDir, p.Cmd, index)...)
	}
	return runTmux(newWindowArgsLinux(session, name, p.WorktreeDir, p.Cmd, index)...)
}

// pairLaunchDir checks p's worktree dir exists and, for a windows-runtime
// pair, returns its win32-form path ("" for linux pairs, which use the WSL
// path directly).
func pairLaunchDir(p RunPair) (winDir string, err error) {
	if err := checkWorktreeDir(p.WorktreeDir); err != nil {
		return "", err
	}
	if p.Runtime != "windows" {
		return "", nil
	}
	return windowsWorktreeWinPath(p.WorktreeDir)
}

// respawnWindow restarts the pane of an existing window (dead, kept by
// remain-on-exit, or alive, which -k kills first) with p's current command
// and directory.
func respawnWindow(windowID string, p RunPair) error {
	winDir, err := pairLaunchDir(p)
	if err != nil {
		return err
	}
	if p.Runtime == "windows" {
		return runTmux(respawnPaneArgsWindows(windowID, winDir, p.Cmd)...)
	}
	return runTmux(respawnPaneArgsLinux(windowID, p.WorktreeDir, p.Cmd)...)
}

// listPanes returns every pane in the session with its live/dead state.
// Returns nil (not an error) if the session doesn't exist.
func listPanes(session string) ([]PaneInfo, error) {
	if !sessionExists(session) {
		return nil, nil
	}
	out, err := tmuxOutput(listPanesArgs(session)...)
	if err != nil {
		return nil, fmt.Errorf("list panes in session %q: %w", session, err)
	}
	return parsePaneLines(out)
}

// capturePane returns a window's pane content (plus up to historyLines of
// scrollback) as text.
func capturePane(pane PaneInfo, historyLines int) (string, error) {
	out, err := tmuxOutput(capturePaneArgs(pane.WindowID, historyLines)...)
	if err != nil {
		return "", fmt.Errorf("capture window %q: %w", pane.Window, err)
	}
	return out, nil
}

// killWindow kills a single window by id.
func killWindow(windowID string) error {
	return runTmux(killWindowArgs(windowID)...)
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
// option isn't set or no tmux server is running (tmux exits non-zero in
// both cases). A real exec failure -- e.g. the tmux binary isn't installed
// -- is returned as an error instead of being mistaken for "nothing active".
func getSessionOption(name string) (value string, found bool, err error) {
	out, err := tmuxOutputQuiet(showSessionOptionArgs(name)...)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("run tmux: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", false, nil
	}
	return out, true, nil
}

// serverRunning reports whether a tmux server is currently running.
func serverRunning() bool {
	return tmuxCommand("list-sessions").Run() == nil
}
