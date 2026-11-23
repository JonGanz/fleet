// Package tmuxsock applies FLEET_TMUX_SOCKET, which points every tmux call
// fleet makes at a private tmux server (`tmux -L <name>`) instead of the
// user's default one. fleet-run uses it for all its tmux calls and
// fleet-task for reading the active ticket, so both see the same server;
// tests set it to stay off the user's real server.
package tmuxsock

import "os"

// Env is the environment variable naming the private tmux socket.
const Env = "FLEET_TMUX_SOCKET"

// Args returns tmux argv for args, prefixed with `-L <socket>` when
// FLEET_TMUX_SOCKET is set (see Argv).
func Args(args ...string) []string {
	return Argv(os.Getenv(Env), args)
}

// Argv prepends `-L <socket>` to args when socket is non-empty. -L must come
// before every other global tmux flag (such as -f), so it is added here,
// once, rather than by each argv builder.
func Argv(socket string, args []string) []string {
	if socket == "" {
		return args
	}
	return append([]string{"-L", socket}, args...)
}
