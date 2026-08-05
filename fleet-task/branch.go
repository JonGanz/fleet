package main

import (
	"regexp"
	"strings"
)

var (
	branchSlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	branchSlugDashes   = regexp.MustCompile(`-{2,}`)
)

// slugifyForBranch turns free text into a compact, git-branch-safe token:
// lowercased, non-alphanumeric runs collapsed to a single "-", leading/
// trailing "-" trimmed, capped to a sane length so a long ticket
// description doesn't produce an unwieldy branch name.
func slugifyForBranch(s string) string {
	s = strings.ToLower(s)
	s = branchSlugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	const maxLen = 40
	if len(s) > maxLen {
		s = strings.Trim(s[:maxLen], "-")
	}
	return s
}

// branchName computes the git branch name for a new ticket worktree from
// template (repos.yaml's branch_template, per-repo or fleet-wide default),
// substituting "{ticket}" and "{description}" (slugified). An empty
// template falls back to "{ticket}" alone — today's plain-ticket behavior.
// Note: if the description differs between two `fleet-task new` runs for
// the same ticket, a template using {description} computes a different
// branch name each time, so the "reattach to the existing branch on rerun"
// fallback in setupWorktree won't find a match and a new branch is created
// instead.
func branchName(template, ticket, description string) string {
	if template == "" {
		template = "{ticket}"
	}
	name := strings.ReplaceAll(template, "{ticket}", ticket)
	name = strings.ReplaceAll(name, "{description}", slugifyForBranch(description))
	name = branchSlugDashes.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	return name
}
