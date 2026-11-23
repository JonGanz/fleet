package main

// Pure decision logic around the active-ticket record, kept separate from
// the tmux-executing glue so it's unit testable (see also stop_logic.go).

// needsSwitch reports whether starting ticket requires tearing down the
// currently running windows first: true only when a *different* ticket is
// recorded as active. Starting the already-active ticket (or starting with
// nothing active) is additive.
func needsSwitch(active string, hasActive bool, ticket string) bool {
	return hasActive && active != ticket
}

// shouldClearActive reports whether the active-ticket record should be
// cleared, given the windows still running in the session: only when none
// remain.
func shouldClearActive(remaining []string) bool {
	return len(remaining) == 0
}
