package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runAttach implements `fleet-run attach [repo:run-name]`.
func runAttach(target string) error {
	window := ""
	if target != "" {
		repo, runName, ok := parsePairLabel(target)
		if !ok {
			return usageErrorf("invalid repo:run-name argument %q (expected the form \"repo:run-name\")", target)
		}
		window = windowName(repo, runName)
	}
	if !interactiveAllowed() {
		return inputRequiredError("attach needs a terminal on stdin; use `fleet-run status` / `fleet-run logs` instead")
	}

	rf, err := reposFile()
	if err != nil {
		return err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return err
	}
	session, err := requireSession(cfg)
	if err != nil {
		return err
	}
	windows, err := listWindows(session)
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		return fmt.Errorf("nothing is running in tmux session %q (see `fleet-run start`)", session)
	}
	if window != "" {
		matches := windowsNamed(windows, window)
		if len(matches) == 0 {
			return fmt.Errorf("%s is not running (no window %q in session %q; see `fleet-run status`)", target, window, session)
		}
		if err := runTmux(selectWindowArgs(matches[0].ID)...); err != nil {
			return fmt.Errorf("select window %q: %w", window, err)
		}
	}

	if insideServer(os.Getenv("TMUX"), serverSocketPath(session)) {
		return runTmux(switchClientArgs(session)...)
	}
	cmd := tmuxCommand(attachSessionArgs(session)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Inside a client of a *different* tmux server (e.g. FLEET_TMUX_SOCKET
	// is set), switch-client can't reach it, and tmux refuses a nested
	// attach while $TMUX is set; dropping it allows the nested attach.
	cmd.Env = envWithout(os.Environ(), "TMUX")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("attach to tmux session %q: %w", session, err)
	}
	return nil
}

// envWithout returns env minus every KEY=... entry for key.
func envWithout(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// insideServer reports whether $TMUX (tmuxEnv, "<socket path>,<pid>,<session>")
// names a client of the server listening on socketPath. Either being empty
// means no.
func insideServer(tmuxEnv, socketPath string) bool {
	if tmuxEnv == "" || socketPath == "" {
		return false
	}
	envSocket, _, _ := strings.Cut(tmuxEnv, ",")
	return filepath.Clean(envSocket) == filepath.Clean(socketPath)
}

// serverSocketPath returns the socket path of the server holding session,
// or "" if it can't be read.
func serverSocketPath(session string) string {
	out, err := tmuxOutputQuiet(socketPathArgs(session)...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
