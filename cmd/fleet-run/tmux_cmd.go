package main

import (
	"fmt"

	"github.com/JonGanz/fleet/internal/wsl"
)

// This file contains pure tmux argv-building functions, kept separate from
// tmux_exec.go's subprocess execution so the command shapes can be unit
// tested without a real tmux binary.
//
// Existing windows are always targeted by their server-unique id ("@N",
// from list-windows/list-panes), never by name: tmux parses a "." in the
// window part of a target as a window.pane separator, so a window named
// "my.app-dev" can be created but not found again by name.

// attachSessionArgs builds argv attaching the current terminal to session.
func attachSessionArgs(session string) []string {
	return []string{"attach-session", "-t", exactTarget(session)}
}

// capturePaneArgs builds argv printing a window's pane content, including up
// to historyLines lines of scrollback, with wrapped lines joined (-J).
func capturePaneArgs(windowID string, historyLines int) []string {
	return []string{"capture-pane", "-p", "-J", "-S", fmt.Sprintf("-%d", historyLines), "-t", windowID}
}

// currentWindowTarget builds "=<session>:", the session's current window.
// Right after new-session that is the only window, whatever base-index the
// user's tmux config sets.
func currentWindowTarget(session string) string {
	return exactTarget(session) + ":"
}

// exactTarget prefixes a tmux target with "=" to force exact-name matching.
// Without it, tmux falls back to prefix matching against every session on
// the server, so a configured session name that happens to prefix-match the
// name of whatever session the caller is currently attached to (or any
// other session) silently targets that session instead — a real bug caught
// against a live tmux server, not a hypothetical.
func exactTarget(session string) string {
	return "=" + session
}

// hasSessionArgs builds argv for `tmux has-session -t <session>`.
func hasSessionArgs(session string) []string {
	return []string{"has-session", "-t", exactTarget(session)}
}

// indexedTarget builds an exact "=<session>:<index>" target for creating a
// window at a specific, caller-computed index.
//
// We compute the index ourselves (see nextFreeWindowIndex) and target it
// explicitly rather than relying on tmux's own placement rules: a bare
// session target (no index) bases placement on the *attached client's*
// current window rather than the target session's, and the "-a" flag
// ("insert after target-window") does the same when the target session
// isn't the one the client is attached to — both were caught, against a
// live tmux server, silently creating windows in the operator's current
// session instead of the configured fleet session.
func indexedTarget(session string, index int) string {
	return fmt.Sprintf("%s:%d", exactTarget(session), index)
}

// killWindowArgs builds argv for killing a window by id.
func killWindowArgs(windowID string) []string {
	return []string{"kill-window", "-t", windowID}
}

// listPanesArgs builds argv listing every pane in the session as
// tab-separated "window_id, window_name, pane_dead, pane_pid,
// pane_dead_status" lines (see parsePaneLines).
func listPanesArgs(session string) []string {
	return []string{"list-panes", "-s", "-t", exactTarget(session), "-F", paneFormat}
}

// listWindowIndicesArgs builds argv for listing window indices in a session,
// used to compute a free index for new-window to target explicitly.
func listWindowIndicesArgs(session string) []string {
	return []string{"list-windows", "-t", exactTarget(session), "-F", "#{window_index}"}
}

// listWindowsArgs builds argv listing a session's windows as tab-separated
// "window_id, window_name" lines (see parseWindowLines).
func listWindowsArgs(session string) []string {
	return []string{"list-windows", "-t", exactTarget(session), "-F", "#{window_id}\t#{window_name}"}
}

// newSessionPrefix builds the leading "-f <file>" global flag (if configFile
// is set) plus the "new-session -d -s <session>" argv shared by both
// runtime variants below.
//
// Note: -f is a global tmux flag, not a `new-session` flag, so it must
// precede the subcommand: `tmux -f <config_file> new-session -d -s <name>`,
// not `tmux new-session -f <config_file> ...`. (tmuxsock.Argv puts -L before it.)
func newSessionPrefix(session, configFile string) []string {
	var args []string
	if configFile != "" {
		args = append(args, "-f", configFile)
	}
	return append(args, "new-session", "-d", "-s", session)
}

// newSessionWithWindowArgsLinux builds argv that creates the fleet session
// and its first window in one atomic tmux call, running cmd directly in cwd
// (the linux-runtime case). Creating the session and the first real window
// together -- rather than a content-less `new-session` followed by a
// separate `new-window` -- avoids tmux's own implicit default window (which
// it names "bash"), so every window in the session is always a real, named
// app window.
func newSessionWithWindowArgsLinux(session, configFile, name, cwd, cmd string) []string {
	args := newSessionPrefix(session, configFile)
	args = append(args, "-n", name, "-c", cwd, cmd)
	return withRemainOnExit(args, currentWindowTarget(session))
}

// newSessionWithWindowArgsWindows builds argv that creates the fleet session
// and its first window in one atomic tmux call, running cmd via
// powershell.exe (the windows-runtime case). winDir must already be the
// win32-form path, per the same requirement as newWindowArgsWindows.
func newSessionWithWindowArgsWindows(session, configFile, name, winDir, cmd string) []string {
	args := newSessionPrefix(session, configFile)
	args = append(args, "-n", name)
	args = append(args, powershellArgv(winDir, cmd)...)
	return withRemainOnExit(args, currentWindowTarget(session))
}

// newWindowArgsLinux builds argv for creating a window that runs cmd
// directly in cwd (the linux-runtime case), at the given explicit index.
func newWindowArgsLinux(session, name, cwd, cmd string, index int) []string {
	target := indexedTarget(session, index)
	args := []string{"new-window", "-t", target, "-n", name, "-c", cwd, cmd}
	return withRemainOnExit(args, target)
}

// newWindowArgsWindows builds argv for creating a window that runs cmd via
// powershell.exe. winDir must already be the win32-form path (e.g. from
// `wslpath -w`), since tmux's own -c expects a WSL-side path and can't be
// used to seed the Windows process's working directory.
func newWindowArgsWindows(session, name, winDir, cmd string, index int) []string {
	target := indexedTarget(session, index)
	args := []string{"new-window", "-t", target, "-n", name}
	args = append(args, powershellArgv(winDir, cmd)...)
	return withRemainOnExit(args, target)
}

// powershellArgv is the command a windows-runtime window runs.
func powershellArgv(winDir, cmd string) []string {
	return []string{"powershell.exe", "-NoExit", "-Command", windowsPowershellCommand(winDir, cmd)}
}

// respawnPaneArgsLinux builds argv that restarts a window's pane (killing
// it first if still running) running cmd in cwd.
func respawnPaneArgsLinux(windowID, cwd, cmd string) []string {
	return []string{"respawn-pane", "-k", "-t", windowID, "-c", cwd, cmd}
}

// respawnPaneArgsWindows builds argv that restarts a window's pane via
// powershell.exe; winDir must already be win32-form (see
// newWindowArgsWindows).
func respawnPaneArgsWindows(windowID, winDir, cmd string) []string {
	return append([]string{"respawn-pane", "-k", "-t", windowID}, powershellArgv(winDir, cmd)...)
}

// selectWindowArgs builds argv making a window its session's current one.
func selectWindowArgs(windowID string) []string {
	return []string{"select-window", "-t", windowID}
}

// setSessionOptionArgs builds argv for setting the @fleet_task_ticket/
// @fleet_task_description user options tracking which ticket is currently
// active.
//
// Uses -g (global session options) rather than -t <session>: set-option/
// show-options resolve their -t as a target-*pane*, which for a bare
// session target (no window/pane component, e.g. "=fleet") can only fall
// back to the attached client's current window -- and right after
// `fleet-run start` creates or targets the session, there is no attached
// client yet, so that resolution fails with a confusing "no such session"
// even though the session exists (caught against a live tmux server, not
// hypothetical). -g sets a server-wide default with no pane/window
// resolution involved at all, which is equivalent in practice since fleet
// only ever has this one session.
func setSessionOptionArgs(name, value string) []string {
	return []string{"set-option", "-g", name, value}
}

// showSessionOptionArgs builds argv for reading a global session option's
// value only (-v), so callers get a bare value with no "name value" prefix
// to strip.
func showSessionOptionArgs(name string) []string {
	return []string{"show-options", "-g", "-v", name}
}

// socketPathArgs builds argv printing the server's socket path, used by
// attach to tell whether $TMUX points at this same server.
func socketPathArgs(session string) []string {
	return []string{"display-message", "-p", "-t", currentWindowTarget(session), "#{socket_path}"}
}

// switchClientArgs builds argv switching the calling tmux client to session.
func switchClientArgs(session string) []string {
	return []string{"switch-client", "-t", exactTarget(session)}
}

// unsetSessionOptionArgs builds argv for clearing a global session option.
func unsetSessionOptionArgs(name string) []string {
	return []string{"set-option", "-gu", name}
}

// windowsPowershellCommand builds the -Command string used to run cmd inside
// a Windows-side shell, in the (already win32-translated) directory.
// -LiteralPath stops PowerShell treating [ ] * ? in the path as wildcards.
func windowsPowershellCommand(winDir, cmd string) string {
	return fmt.Sprintf("Set-Location -LiteralPath %s; %s", wsl.PSQuote(winDir), cmd)
}

// withRemainOnExit appends a chained `; set-option -w -t <target>
// remain-on-exit on` to a window-creating argv. Chaining it into the same
// tmux invocation (rather than a second tmux call) applies the option in the
// same server command queue as the window's creation, so even a command that
// exits immediately leaves a dead pane behind -- with its output, readable
// via `fleet-run logs` -- instead of the window silently vanishing. target
// is the index/current-window form used for the creation, not the window
// name (see the file comment).
func withRemainOnExit(args []string, target string) []string {
	return append(args, ";", "set-option", "-w", "-t", target, "remain-on-exit", "on")
}
