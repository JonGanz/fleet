package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// fzfSelect pipes items (one per line) into `fzf`, wired so fzf draws its
// UI against the real terminal (stdin/stderr inherited) while its stdout
// (the selected line(s)) is captured into a buffer instead of the
// terminal. fzf itself talks to /dev/tty directly for its UI when stdout
// is redirected, so this composes fine. extraArgs lets callers add flags
// such as "--multi". Returns the selected lines (trimmed, empty lines
// dropped). If fzf exits non-zero (e.g. the user pressed Esc, or there
// were no candidates), returns an empty slice and a non-nil error so
// callers can distinguish "cancelled" from "chose nothing".
func fzfSelect(items []string, extraArgs ...string) ([]string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return nil, fmt.Errorf("fzf not found on PATH: %w", err)
	}

	cmd := exec.Command("fzf", extraArgs...)
	cmd.Stdin = strings.NewReader(strings.Join(items, "\n"))
	cmd.Stderr = os.Stderr

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("fzf: %w", err)
	}

	var selected []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		selected = append(selected, line)
	}
	return selected, nil
}

// fzfSelectMulti is fzfSelect with --multi.
func fzfSelectMulti(items []string) ([]string, error) {
	return fzfSelect(items, "--multi")
}

// fzfSelectOne is fzfSelect for single-selection use, returning the sole
// selected line (or an error if none was selected/cancelled).
func fzfSelectOne(items []string) (string, error) {
	selected, err := fzfSelect(items)
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("no selection made")
	}
	return selected[0], nil
}
