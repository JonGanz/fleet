package main

import (
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/taskstate"
)

func TestBuildJumpEntries(t *testing.T) {
	tasks := []*taskstate.State{
		{
			Ticket: "PROJ-1",
			Repos: []taskstate.Repo{
				{Repo: "backend", WorktreePath: "/wt/PROJ-1/backend"},
				{Repo: "admin-ui", WorktreePath: "/wt/PROJ-1/admin-ui"},
			},
		},
		{
			Ticket: "PROJ-2",
			Repos: []taskstate.Repo{
				{Repo: "backend", WorktreePath: "/wt/PROJ-2/backend"},
			},
		},
	}

	entries := buildJumpEntries(tasks)
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	want := jumpEntry{Ticket: "PROJ-1", Repo: "backend", WorktreePath: "/wt/PROJ-1/backend"}
	if entries[0] != want {
		t.Errorf("entries[0] = %+v, want %+v", entries[0], want)
	}

	lines := jumpEntryLines(entries)
	if len(lines) != len(entries) {
		t.Fatalf("len(jumpEntryLines(entries)) = %d, want %d", len(lines), len(entries))
	}
	for _, l := range lines {
		if strings.Contains(l, "\t") {
			t.Errorf("line %q contains a raw tab; columns should be space-padded", l)
		}
	}
	if !strings.Contains(lines[0], "PROJ-1") || !strings.Contains(lines[0], "backend") || !strings.Contains(lines[0], "/wt/PROJ-1/backend") {
		t.Errorf("lines[0] = %q, missing expected fields", lines[0])
	}
}

func TestFilterJumpEntries(t *testing.T) {
	entries := []jumpEntry{{Ticket: "T", Repo: "api"}, {Ticket: "T", Repo: "ui"}}
	got := filterJumpEntries(entries, "ui")
	if len(got) != 1 || got[0].Repo != "ui" {
		t.Errorf("filterJumpEntries = %v", got)
	}
	if got := filterJumpEntries(entries, "db"); len(got) != 0 {
		t.Errorf("unknown repo should match nothing, got %v", got)
	}
}

func TestFormatPatchList(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{{nil, "?"}, {[]string{}, "-"}, {[]string{"a.patch", "b.patch"}, "a.patch,b.patch"}}
	for _, c := range cases {
		if got := formatPatchList(c.in); got != c.want {
			t.Errorf("formatPatchList(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}
