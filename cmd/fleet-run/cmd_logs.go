package main

import (
	"fmt"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/xdg"
)

// maxLogLines caps --lines so a typo can't ask tmux for an absurd capture.
const maxLogLines = 100000

// LogsReport is the `fleet-run logs --json` output.
type LogsReport struct {
	Target string   `json:"target"`
	Window string   `json:"window"`
	Alive  bool     `json:"alive"`
	Lines  []string `json:"lines"`
}

// deadPaneMarker starts the line tmux writes at the bottom of a pane whose
// process exited (kept by remain-on-exit).
const deadPaneMarker = "Pane is dead"

// lastLines splits captured pane text into lines, drops trailing blank lines
// (the unused bottom of the pane), and keeps at most n from the end. For a
// dead pane tmux writes its "Pane is dead" line at the very bottom, so the
// unused blank rows sit between the app's output and that line; they're
// dropped too, so they don't use up the n-line budget.
func lastLines(captured string, n int) []string {
	lines := trimTrailingBlank(strings.Split(captured, "\n"))
	if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], deadPaneMarker) {
		marker := lines[len(lines)-1]
		lines = append(trimTrailingBlank(lines[:len(lines)-1]), marker)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// runLogs implements `fleet-run logs <repo:run-name> [--lines N] [--json]`.
// Works on dead panes too (kept by remain-on-exit), which is how a crashed
// app's output is read.
func runLogs(label string, lines int, asJSON bool) error {
	repo, runName, ok := parsePairLabel(label)
	if !ok {
		return cli.Usagef("invalid repo:run-name argument %q (expected the form \"repo:run-name\")", label)
	}
	if lines < 1 || lines > maxLogLines {
		return cli.Usagef("--lines must be between 1 and %d (got %d)", maxLogLines, lines)
	}

	rf, err := xdg.ReposFile()
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

	window := windowName(repo, runName)
	panes, err := listPanes(session)
	if err != nil {
		return err
	}
	pane, ok := findPane(panes, window)
	if !ok {
		return fmt.Errorf("%s is not running (no window %q in session %q; see `fleet-run status`)", label, window, session)
	}
	captured, err := capturePane(pane, lines)
	if err != nil {
		return err
	}
	report := LogsReport{Target: label, Window: window, Alive: pane.Alive, Lines: lastLines(captured, lines)}
	if asJSON {
		return cli.PrintJSON(report)
	}
	for _, l := range report.Lines {
		fmt.Println(l)
	}
	return nil
}

// trimTrailingBlank drops whitespace-only lines from the end.
func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
