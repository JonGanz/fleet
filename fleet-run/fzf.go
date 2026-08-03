package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runFzf pipes candidates (one per line) into fzf with the given extra args
// (e.g. "--multi") and returns the selected lines.
//
// Wiring: stdin carries the candidate list, stderr is inherited so fzf's
// interactive UI is visible, and stdout is captured to collect the
// selection. This is the standard fzf pipeline shape (`fzf < input >
// output`) -- fzf itself opens /dev/tty directly for keyboard input and
// screen drawing regardless of how stdin/stdout are redirected, so capturing
// stdout for the result does not interfere with the interactive UI. No
// fallback to manually opening /dev/tty was needed.
//
// Returns an empty (nil) slice, not an error, if the user aborts the
// selection (fzf exits 130) or picks nothing.
func runFzf(candidates []string, args ...string) ([]string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return nil, fmt.Errorf("fzf not found in PATH: %w", err)
	}

	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(strings.Join(candidates, "\n") + "\n")
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// 130 = interrupted (Ctrl-C), 1 = no match / user aborted with
			// Esc: both mean "nothing selected", not a hard failure.
			if code := exitErr.ExitCode(); code == 130 || code == 1 {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("run fzf: %w", err)
	}

	return splitNonEmptyLines(out.String()), nil
}

// multiSelect runs fzf in multiselect mode over candidates.
func multiSelect(candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	return runFzf(candidates, "--multi")
}
