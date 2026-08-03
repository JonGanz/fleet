package main

import "strings"

// windowName builds the tmux window name for a given ticket/repo/run-name
// triple, per the contract's naming convention: "<ticket>-<repo>-<run-name>".
func windowName(ticket, repo, runName string) string {
	return ticket + "-" + repo + "-" + runName
}

// windowPrefix returns the "<ticket>-" prefix used to identify all windows
// belonging to a ticket, regardless of repo/run-name.
func windowPrefix(ticket string) string {
	return ticket + "-"
}

// ParsedWindow is the decomposed form of a tmux window name.
type ParsedWindow struct {
	Ticket  string
	Repo    string
	RunName string
}

// parseWindowName decomposes a window name of the form
// "<ticket>-<repo>-<run-name>" back into its components.
//
// Ambiguity note: hyphens are both the field separator and a character
// commonly present *within* ticket ids (e.g. "PROJ-1234"), repo names, and
// run names, so a bare "a-b-c-d" string alone does not uniquely decompose.
// To disambiguate we require the caller to supply the known set of ticket
// ids and repo names currently in play (e.g. from tasks/*.json and
// repos.yaml); we then:
//
//  1. Find the known ticket that is a prefix of name (longest match wins, so
//     "PROJ-1" doesn't shadow "PROJ-12" if both happen to exist).
//  2. Strip "<ticket>-", then find the known repo that is a prefix of the
//     remainder (again longest match wins, so "api" doesn't shadow
//     "api-gateway").
//  3. Whatever remains after "<repo>-" is the run name verbatim, hyphens and
//     all.
//
// If no known ticket/repo combination matches, ok is false. This means a
// window created by a stale/removed ticket or repo config cannot be parsed
// back precisely -- callers that only need to test "does this window belong
// to ticket X" should use hasWindowPrefix instead, which only needs the
// ticket and has no such ambiguity.
func parseWindowName(name string, knownTickets, knownRepos []string) (ParsedWindow, bool) {
	ticket, rest, ok := stripLongestPrefix(name, knownTickets, "-")
	if !ok {
		return ParsedWindow{}, false
	}
	repo, runName, ok := stripLongestPrefix(rest, knownRepos, "-")
	if !ok {
		return ParsedWindow{}, false
	}
	return ParsedWindow{Ticket: ticket, Repo: repo, RunName: runName}, true
}

// stripLongestPrefix finds the longest candidate in candidates such that
// s == candidate+sep+rest, returning (candidate, rest, true), or
// ("", "", false) if none match.
func stripLongestPrefix(s string, candidates []string, sep string) (string, string, bool) {
	best := -1
	for _, c := range candidates {
		p := c + sep
		if strings.HasPrefix(s, p) && len(c) > best {
			best = len(c)
		}
	}
	if best == -1 {
		return "", "", false
	}
	for _, c := range candidates {
		if len(c) != best {
			continue
		}
		p := c + sep
		if strings.HasPrefix(s, p) {
			return c, strings.TrimPrefix(s, p), true
		}
	}
	return "", "", false
}

// hasWindowPrefix reports whether window name belongs to ticket, i.e. starts
// with "<ticket>-". Unlike parseWindowName this needs no knowledge of repo
// names and is unambiguous, so it's the preferred check for filtering
// windows to stop/kill.
func hasWindowPrefix(name, ticket string) bool {
	return strings.HasPrefix(name, windowPrefix(ticket))
}

// filterWindowsByTicketPrefix returns the subset of windows whose name
// starts with "<ticket>-". If ticket is empty, all windows are returned
// (caller is expected to have already warned about the lack of a filter).
func filterWindowsByTicketPrefix(windows []string, ticket string) []string {
	if ticket == "" {
		out := make([]string, len(windows))
		copy(out, windows)
		return out
	}
	var out []string
	for _, w := range windows {
		if hasWindowPrefix(w, ticket) {
			out = append(out, w)
		}
	}
	return out
}
