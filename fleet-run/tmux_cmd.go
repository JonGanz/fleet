package main

import "fmt"

// This file contains pure tmux argv-building functions, kept separate from
// tmux_exec.go's subprocess execution so the command shapes can be unit
// tested without a real tmux binary.

// hasSessionArgs builds argv for `tmux has-session -t <session>`.
func hasSessionArgs(session string) []string {
	return []string{"has-session", "-t", session}
}

// newSessionArgv builds the full argv (including the leading "-f <file>"
// global flag, if configFile is set) for creating the fixed fleet session.
//
// Note: -f is a global tmux flag, not a `new-session` flag, so it must
// precede the subcommand: `tmux -f <config_file> new-session -d -s <name>`,
// not `tmux new-session -f <config_file> ...`.
func newSessionArgv(session, configFile string) []string {
	var args []string
	if configFile != "" {
		args = append(args, "-f", configFile)
	}
	args = append(args, "new-session", "-d", "-s", session)
	return args
}

// listWindowsArgs builds argv for listing window names in a session.
func listWindowsArgs(session string) []string {
	return []string{"list-windows", "-t", session, "-F", "#{window_name}"}
}

// newWindowArgsLinux builds argv for creating a window that runs cmd
// directly in cwd (the linux-runtime case).
//
// -a inserts the window after the current one, shifting other windows up
// if necessary, instead of tmux's default of erroring with "index N in
// use" when that slot is already occupied by another ticket's window.
func newWindowArgsLinux(session, name, cwd, cmd string) []string {
	return []string{"new-window", "-a", "-t", session, "-n", name, "-c", cwd, cmd}
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
func newWindowArgsWindows(session, name, winDir, cmd string) []string {
	psCmd := windowsPowershellCommand(winDir, cmd)
	return []string{"new-window", "-a", "-t", session, "-n", name, "powershell.exe", "-NoExit", "-Command", psCmd}
}

// killWindowArgs builds argv for killing a specific window by name.
func killWindowArgs(session, name string) []string {
	return []string{"kill-window", "-t", session + ":" + name}
}
