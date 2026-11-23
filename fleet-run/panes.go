package main

import (
	"fmt"
	"strconv"
	"strings"
)

// PaneInfo is one window's pane state, as reported by `fleet-run status`.
// Every fleet window has exactly one pane.
type PaneInfo struct {
	Window string `json:"window"`
	// Target is the "repo:run-name" form, or "" for a window repos.yaml no
	// longer describes.
	Target string `json:"target"`
	Alive  bool   `json:"alive"`
	PID    int    `json:"pid"`
	// ExitStatus is set only for a dead pane (kept by remain-on-exit).
	ExitStatus *int `json:"exit_status,omitempty"`
}

// findPane returns the pane for window, if any.
func findPane(panes []PaneInfo, window string) (PaneInfo, bool) {
	for _, p := range panes {
		if p.Window == window {
			return p, true
		}
	}
	return PaneInfo{}, false
}

// labelPanes fills in each pane's Target from labels (window name ->
// "repo:run-name"; see windowLabels).
func labelPanes(panes []PaneInfo, labels map[string]string) []PaneInfo {
	out := make([]PaneInfo, len(panes))
	for i, p := range panes {
		p.Target = labels[p.Window]
		out[i] = p
	}
	return out
}

// parsePaneLine parses one line of listPanesArgs output. The window name is
// everything before the last three tab-separated fields, so a (never
// expected) tab inside a window name can't shift the other fields.
func parsePaneLine(line string) (PaneInfo, error) {
	fields := strings.Split(line, "\t")
	if len(fields) < 4 {
		return PaneInfo{}, fmt.Errorf("unexpected tmux pane line %q", line)
	}
	n := len(fields)
	window := strings.Join(fields[:n-3], "\t")
	deadField, pidField, statusField := fields[n-3], fields[n-2], fields[n-1]

	if deadField != "0" && deadField != "1" {
		return PaneInfo{}, fmt.Errorf("unexpected pane_dead %q in tmux pane line %q", deadField, line)
	}
	pid, err := strconv.Atoi(pidField)
	if err != nil {
		return PaneInfo{}, fmt.Errorf("unexpected pane_pid %q in tmux pane line %q", pidField, line)
	}
	p := PaneInfo{Window: window, Alive: deadField == "0", PID: pid}
	if !p.Alive && statusField != "" {
		status, err := strconv.Atoi(statusField)
		if err != nil {
			return PaneInfo{}, fmt.Errorf("unexpected pane_dead_status %q in tmux pane line %q", statusField, line)
		}
		p.ExitStatus = &status
	}
	return p, nil
}

// parsePaneLines parses listPanesArgs output, skipping blank lines.
func parsePaneLines(out string) ([]PaneInfo, error) {
	var panes []PaneInfo
	for _, line := range splitNonEmptyLines(out) {
		p, err := parsePaneLine(line)
		if err != nil {
			return nil, err
		}
		panes = append(panes, p)
	}
	return panes, nil
}
