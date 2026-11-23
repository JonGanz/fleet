package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	branchSlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	branchSlugDashes   = regexp.MustCompile(`-{2,}`)
)

// branchName computes the git branch name for a new ticket worktree from
// template (repos.yaml's branch_template, per-repo or fleet-wide default),
// substituting "{ticket}" and "{description}" (slugified). An empty
// template falls back to "{ticket}" alone — today's plain-ticket behavior.
// Note: if the description differs between two `fleet-task new` runs for
// the same ticket, a template using {description} computes a different
// branch name each time, so planWorktreeAdd won't find the earlier branch
// to reattach and a new branch is created instead.
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

// isValidBranchName asks git whether name is a valid branch name. The
// printed name must also match: `--branch` expands shorthands like @{-1}
// when run inside a repository, which would otherwise pass.
func isValidBranchName(name string) (bool, error) {
	got, err := gitOutput(nil, "check-ref-format", "--branch", name)
	if err != nil {
		if gitExitCode(err) > 0 {
			return false, nil
		}
		return false, fmt.Errorf("running git check-ref-format: %w", err)
	}
	return got == name, nil
}

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
