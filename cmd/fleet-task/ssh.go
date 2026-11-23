package main

import "strings"

// isSSHURL reports whether a git remote URL looks like an SSH URL, per the
// contract's "git@... or ssh://..." heuristic.
func isSSHURL(origin string) bool {
	return strings.HasPrefix(origin, "git@") || strings.HasPrefix(origin, "ssh://")
}
