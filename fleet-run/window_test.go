package main

import (
	"reflect"
	"testing"
)

func TestWindowName(t *testing.T) {
	got := windowName("PROJ-1234", "backend", "api")
	want := "PROJ-1234-backend-api"
	if got != want {
		t.Errorf("windowName = %q, want %q", got, want)
	}
}

func TestHasWindowPrefix(t *testing.T) {
	cases := []struct {
		name   string
		ticket string
		want   bool
	}{
		{"PROJ-1234-backend-api", "PROJ-1234", true},
		{"PROJ-12345-backend-api", "PROJ-1234", false}, // no false positive on ticket substring
		{"OTHER-1-backend-api", "PROJ-1234", false},
	}
	for _, c := range cases {
		if got := hasWindowPrefix(c.name, c.ticket); got != c.want {
			t.Errorf("hasWindowPrefix(%q, %q) = %v, want %v", c.name, c.ticket, got, c.want)
		}
	}
}

func TestParseWindowName(t *testing.T) {
	knownTickets := []string{"PROJ-1234", "PROJ-1"}
	knownRepos := []string{"backend", "admin-ui", "api", "api-gateway"}

	cases := []struct {
		name string
		want ParsedWindow
		ok   bool
	}{
		{
			name: "PROJ-1234-backend-api",
			want: ParsedWindow{Ticket: "PROJ-1234", Repo: "backend", RunName: "api"},
			ok:   true,
		},
		{
			// longest-ticket-match: PROJ-1234 should win over PROJ-1
			name: "PROJ-1234-admin-ui-dev",
			want: ParsedWindow{Ticket: "PROJ-1234", Repo: "admin-ui", RunName: "dev"},
			ok:   true,
		},
		{
			// longest-repo-match: api-gateway should win over api
			name: "PROJ-1234-api-gateway-dev",
			want: ParsedWindow{Ticket: "PROJ-1234", Repo: "api-gateway", RunName: "dev"},
			ok:   true,
		},
		{
			// run name itself contains hyphens; whatever remains after
			// ticket/repo is stripped is taken verbatim.
			name: "PROJ-1234-backend-start-dev-watch",
			want: ParsedWindow{Ticket: "PROJ-1234", Repo: "backend", RunName: "start-dev-watch"},
			ok:   true,
		},
		{
			name: "UNKNOWN-1-backend-api",
			ok:   false,
		},
		{
			name: "PROJ-1234-unknown-repo-api",
			ok:   false,
		},
	}

	for _, c := range cases {
		got, ok := parseWindowName(c.name, knownTickets, knownRepos)
		if ok != c.ok {
			t.Errorf("parseWindowName(%q) ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseWindowName(%q) = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestFilterWindowsByTicketPrefix(t *testing.T) {
	windows := []string{
		"PROJ-1234-backend-api",
		"PROJ-1234-admin-ui-dev",
		"PROJ-9999-backend-api",
		"PROJ-12345-backend-worker", // shares ticket prefix chars but different ticket
	}

	got := filterWindowsByTicketPrefix(windows, "PROJ-1234")
	want := []string{"PROJ-1234-backend-api", "PROJ-1234-admin-ui-dev"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterWindowsByTicketPrefix = %v, want %v", got, want)
	}

	all := filterWindowsByTicketPrefix(windows, "")
	if !reflect.DeepEqual(all, windows) {
		t.Errorf("filterWindowsByTicketPrefix(empty ticket) = %v, want all windows %v", all, windows)
	}
}
