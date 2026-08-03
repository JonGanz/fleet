package main

import (
	"fmt"
	"strings"
)

// This file holds the pure decision logic for `fleet-run stop`, kept
// separate from cmd_stop.go's IO/subprocess glue so it's unit testable.

// validateAllFlags enforces the safety rule for `--all`: killing every
// window across every ticket requires an explicit extra confirmation
// (--everything) when no --ticket was given, so a bare `fleet-run stop
// --all` can't accidentally nuke other tickets' windows.
func validateAllFlags(ticket string, everything bool) error {
	if ticket == "" && !everything {
		return fmt.Errorf("`--all` without `--ticket` would stop every window in the session, across every ticket; pass `--ticket <id>` to scope it, or add `--everything` to confirm you really want to stop all windows for all tickets")
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
// their corresponding tmux window names for the given ticket.
func namesToWindowNames(names []string, ticket string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		repo, runName, ok := parsePairLabel(n)
		if !ok {
			return nil, fmt.Errorf("invalid repo:run-name argument %q (expected the form \"repo:run-name\")", n)
		}
		out = append(out, windowName(ticket, repo, runName))
	}
	return out, nil
}

// stopSelection is a pure decision function for `fleet-run stop`, given the
// currently running windows and the parsed flags/args. It returns the exact
// window names that should be killed, or an error. It does not itself
// resolve fzf multiselect (that requires interactive IO) -- when names is
// empty and all is false, the caller is expected to have already run
// multiselect over candidateWindowsForStop and pass the result in as names'
// resolved window names via preselected.
func stopSelection(windows []string, ticket string, all, everything bool, resolvedWindowNames []string) ([]string, error) {
	if all {
		if err := validateAllFlags(ticket, everything); err != nil {
			return nil, err
		}
		return filterWindowsByTicketPrefix(windows, ticket), nil
	}
	// Either explicit names (already resolved to window names by the
	// caller) or a prior fzf multiselect result -- both arrive the same way.
	return resolvedWindowNames, nil
}

// candidateWindowsForStop returns the windows eligible for the fzf
// multiselect fallback: all currently running windows, filtered to the
// ticket's prefix if a ticket was given.
func candidateWindowsForStop(windows []string, ticket string) []string {
	return filterWindowsByTicketPrefix(windows, ticket)
}
