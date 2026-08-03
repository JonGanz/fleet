package main

import "testing"

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

	line := entries[0].line()
	if line != "PROJ-1\tbackend\t/wt/PROJ-1/backend" {
		t.Errorf("line() = %q", line)
	}
}
