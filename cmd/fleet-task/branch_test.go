package main

import "testing"

func TestBranchName(t *testing.T) {
	cases := []struct {
		name        string
		template    string
		ticket      string
		description string
		want        string
	}{
		{"empty template falls back to plain ticket", "", "PROJ-123", "Fix login bug", "PROJ-123"},
		{"ticket-only template", "{ticket}", "PROJ-123", "Fix login bug", "PROJ-123"},
		{"static prefix", "eng/{ticket}", "PROJ-123", "Fix login bug", "eng/PROJ-123"},
		{"ticket and description", "{ticket}-{description}", "PROJ-123", "Fix login bug", "PROJ-123-fix-login-bug"},
		{"empty description collapses cleanly", "{ticket}-{description}", "PROJ-123", "", "PROJ-123"},
		{"description with punctuation", "{ticket}-{description}", "PROJ-123", "Fix: the login/logout bug!!", "PROJ-123-fix-the-login-logout-bug"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := branchName(tc.template, tc.ticket, tc.description)
			if got != tc.want {
				t.Errorf("branchName(%q, %q, %q) = %q, want %q", tc.template, tc.ticket, tc.description, got, tc.want)
			}
		})
	}
}

func TestSlugifyForBranch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Fix login bug", "fix-login-bug"},
		{"  leading/trailing spaces  ", "leading-trailing-spaces"},
		{"ALL CAPS!!", "all-caps"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := slugifyForBranch(tc.in); got != tc.want {
			t.Errorf("slugifyForBranch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	long := "this is a very long description that goes on and on and on and on and on and on and on and on"
	got := slugifyForBranch(long)
	if len(got) > 40 {
		t.Errorf("slugifyForBranch(long) = %q, len %d, want <= 40", got, len(got))
	}
	if got == "" || got[len(got)-1] == '-' {
		t.Errorf("slugifyForBranch(long) = %q, should not end in a trailing dash after truncation", got)
	}
}
