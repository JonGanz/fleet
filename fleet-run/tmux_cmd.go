package main

import "fmt"

// This file contains pure tmux argv-building functions, kept separate from
// tmux_exec.go's subprocess execution so the command shapes can be unit
// tested without a real tmux binary.

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

// newSessionPrefix builds the leading "-f <file>" global flag (if configFile
// is set) plus the "new-session -d -s <session>" argv shared by both
// runtime variants below.
//
// Note: -f is a global tmux flag, not a `new-session` flag, so it must
// precede the subcommand: `tmux -f <config_file> new-session -d -s <name>`,
// not `tmux new-session -f <config_file> ...`.
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
	return withRemainOnExit(args, session, name)
}

// newSessionWithWindowArgsWindows builds argv that creates the fleet session
// and its first window in one atomic tmux call, running cmd via
// powershell.exe (the windows-runtime case). winDir must already be the
// win32-form path, per the same requirement as newWindowArgsWindows.
func newSessionWithWindowArgsWindows(session, configFile, name, winDir, cmd string) []string {
	psCmd := windowsPowershellCommand(winDir, cmd)
	args := newSessionPrefix(session, configFile)
	args = append(args, "-n", name, "powershell.exe", "-NoExit", "-Command", psCmd)
	return withRemainOnExit(args, session, name)
}

// listWindowsArgs builds argv for listing window names in a session.
func listWindowsArgs(session string) []string {
	return []string{"list-windows", "-t", exactTarget(session), "-F", "#{window_name}"}
}

// listWindowIndicesArgs builds argv for listing window indices in a session,
// used to compute a free index for new-window to target explicitly.
func listWindowIndicesArgs(session string) []string {
	return []string{"list-windows", "-t", exactTarget(session), "-F", "#{window_index}"}
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

// newWindowArgsLinux builds argv for creating a window that runs cmd
// directly in cwd (the linux-runtime case), at the given explicit index.
func newWindowArgsLinux(session, name, cwd, cmd string, index int) []string {
	args := []string{"new-window", "-t", indexedTarget(session, index), "-n", name, "-c", cwd, cmd}
	return withRemainOnExit(args, session, name)
}

// windowsPowershellCommand builds the -Command string used to run cmd inside
// a Windows-side shell, cd'd into the (already win32-translated) directory.
func windowsPowershellCommand(winDir, cmd string) string {
	return fmt.Sprintf("cd '%s'; %s", winDir, cmd)
}

// newWindowArgsWindows builds argv for creating a window that runs cmd via
// powershell.exe. winDir must already be the win32-form path (e.g. from
// `wslpath -w`), since tmux's own -c expects a WSL-side path and can't be
// used to seed the Windows process's working directory.
func newWindowArgsWindows(session, name, winDir, cmd string, index int) []string {
	psCmd := windowsPowershellCommand(winDir, cmd)
	args := []string{"new-window", "-t", indexedTarget(session, index), "-n", name, "powershell.exe", "-NoExit", "-Command", psCmd}
	return withRemainOnExit(args, session, name)
}

// windowTarget builds an exact "=<session>:<window name>" target.
func windowTarget(session, name string) string {
	return exactTarget(session) + ":" + name
}

// withRemainOnExit appends a chained `; set-option -w -t <window>
// remain-on-exit on` to a window-creating argv. Chaining it into the same
// tmux invocation (rather than a second tmux call) applies the option in the
// same server command queue as the window's creation, so even a command that
// exits immediately leaves a dead pane behind -- with its output, readable
// via `fleet-run logs` -- instead of the window silently vanishing.
func withRemainOnExit(args []string, session, name string) []string {
	return append(args, ";", "set-option", "-w", "-t", windowTarget(session, name), "remain-on-exit", "on")
}

// respawnPaneArgsLinux builds argv that restarts a window's dead pane
// (kept by remain-on-exit) running cmd in cwd.
func respawnPaneArgsLinux(session, name, cwd, cmd string) []string {
	return []string{"respawn-pane", "-k", "-t", windowTarget(session, name), "-c", cwd, cmd}
}

// respawnPaneArgsWindows builds argv that restarts a window's dead pane via
// powershell.exe; winDir must already be win32-form (see
// newWindowArgsWindows).
func respawnPaneArgsWindows(session, name, winDir, cmd string) []string {
	psCmd := windowsPowershellCommand(winDir, cmd)
	return []string{"respawn-pane", "-k", "-t", windowTarget(session, name), "powershell.exe", "-NoExit", "-Command", psCmd}
}

// listPanesArgs builds argv listing every pane in the session as
// tab-separated "window_name, pane_dead, pane_pid, pane_dead_status" lines
// (see parsePaneLines).
func listPanesArgs(session string) []string {
	return []string{"list-panes", "-s", "-t", exactTarget(session), "-F", paneFormat}
}

// paneFormat is the -F format parsed by parsePaneLines.
const paneFormat = "#{window_name}\t#{pane_dead}\t#{pane_pid}\t#{pane_dead_status}"

// capturePaneArgs builds argv printing a window's pane content, including up
// to historyLines lines of scrollback, with wrapped lines joined (-J).
func capturePaneArgs(session, name string, historyLines int) []string {
	return []string{"capture-pane", "-p", "-J", "-S", fmt.Sprintf("-%d", historyLines), "-t", windowTarget(session, name)}
}

// killWindowArgs builds argv for killing a specific window by name.
func killWindowArgs(session, name string) []string {
	return []string{"kill-window", "-t", windowTarget(session, name)}
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

// unsetSessionOptionArgs builds argv for clearing a global session option.
func unsetSessionOptionArgs(name string) []string {
	return []string{"set-option", "-gu", name}
}

// showSessionOptionArgs builds argv for reading a global session option's
// value only (-v), so callers get a bare value with no "name value" prefix
// to strip.
func showSessionOptionArgs(name string) []string {
	return []string{"show-options", "-g", "-v", name}
}
