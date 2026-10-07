package main

import "testing"

func TestValidateTicketName(t *testing.T) {
	cases := []struct {
		ticket  string
		wantErr bool
	}{
		{"PROJ-1234", false},
		{"", true},
		{".", true},
		{"..", true},
		{"a/b", true},
		{`a\b`, true},
		{"../escape", true},
	}
	for _, c := range cases {
		err := validateTicketName(c.ticket)
		if (err != nil) != c.wantErr {
			t.Errorf("validateTicketName(%q): err = %v, wantErr = %v", c.ticket, err, c.wantErr)
		}
	}
}

func TestRepoNames(t *testing.T) {
	repos := []TaskRepo{{Repo: "backend"}, {Repo: "frontend"}}
	if got, want := repoNames(repos), "backend, frontend"; got != want {
		t.Errorf("repoNames() = %q, want %q", got, want)
	}
	if got, want := repoNames(nil), ""; got != want {
		t.Errorf("repoNames(nil) = %q, want %q", got, want)
	}
}
