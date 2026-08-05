package main

import (
	"strings"
	"testing"
)

func TestBuildJumpEntries(t *testing.T) {
	tasks := []*TaskState{
		{
			Ticket: "PROJ-1",
			Repos: []TaskRepo{
				{Repo: "backend", WorktreePath: "/wt/PROJ-1/backend"},
				{Repo: "admin-ui", WorktreePath: "/wt/PROJ-1/admin-ui"},
			},
		},
		{
			Ticket: "PROJ-2",
			Repos: []TaskRepo{
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
