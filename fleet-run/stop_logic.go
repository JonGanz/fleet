package main

import (
	"fmt"
	"strings"
)

// This file holds the pure decision logic for `fleet-run stop`, kept
// separate from cmd_stop.go's IO/subprocess glue so it's unit testable.

// checkTicketMatchesActive enforces the (optional) `--ticket` safety check:
// since only one ticket's windows are ever live at once, --ticket is no
// longer a filter -- it's an assertion that the ticket you think is running
// is actually the one that's running, so a bare `stop --all` fired against
// the wrong mental model errors out instead of silently stopping someone
// else's context.
func checkTicketMatchesActive(ticket, active string, hasActive bool) error {
	if !hasActive {
		return fmt.Errorf("--ticket %s given, but no ticket is currently active (nothing running)", ticket)
	}
	if active != ticket {
		return fmt.Errorf("--ticket %s given, but %s is the currently active ticket; nothing to stop for %s", ticket, active, ticket)
	}
	return nil
}

// parsePairLabel splits a "repo:run-name" positional argument into its two
// parts.
func parsePairLabel(label string) (repo, runName string, ok bool) {
	repo, runName, found := strings.Cut(label, ":")
	if !found || repo == "" || runName == "" {
		return "", "", false
	}
	return repo, runName, true
}

// namesToWindowNames converts a list of "repo:run-name" positional args into
// their corresponding tmux window names.
func namesToWindowNames(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		repo, runName, ok := parsePairLabel(n)
		if !ok {
			return nil, fmt.Errorf("invalid repo:run-name argument %q (expected the form \"repo:run-name\")", n)
		}
		out = append(out, windowName(repo, runName))
	}
	return out, nil
}

// windowLabels maps every tmux window name derivable from cfg back to its
// "repo:run-name" label (the CLI arg form). Config validation guarantees the
// window names are unique, so the mapping is unambiguous.
func windowLabels(cfg *ReposConfig) map[string]string {
	m := make(map[string]string)
	if cfg == nil {
		return m
	}
	for _, r := range cfg.Repos {
		for _, run := range r.Run {
			m[windowName(r.Name, run.Name)] = r.Name + ":" + run.Name
		}
	}
	return m
}

// pickerItems converts running window names into picker rows, showing the
// "repo:run-name" form where known and the raw window name otherwise (e.g. a
// window whose repo was since removed from repos.yaml).
func pickerItems(windows []string, labels map[string]string) []string {
	out := make([]string, len(windows))
	for i, w := range windows {
		if l, ok := labels[w]; ok {
			out[i] = l
		} else {
			out[i] = w
		}
	}
	return out
}

// pickerSelectionToWindows maps picker rows back to window names.
func pickerSelectionToWindows(selected, windows []string, labels map[string]string) []string {
	byItem := make(map[string]string, len(windows))
	for i, it := range pickerItems(windows, labels) {
		byItem[it] = windows[i]
	}
	out := make([]string, 0, len(selected))
	for _, s := range selected {
		if w, ok := byItem[s]; ok {
			out = append(out, w)
		} else {
			out = append(out, s)
		}
	}
	return out
}
