package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks", "PROJ-1234.json")

	want := &TaskState{
		Ticket:      "PROJ-1234",
		Description: "Add retry logic to payment webhook",
		CreatedAt:   time.Date(2026, 8, 2, 22, 58, 0, 0, time.UTC),
		Repos: []TaskRepo{
			{Repo: "backend", Branch: "PROJ-1234", WorktreePath: "/home/jon/.local/state/fleet/worktrees/PROJ-1234/backend"},
			{Repo: "admin-ui", Branch: "PROJ-1234", WorktreePath: "/home/jon/.local/state/fleet/worktrees/PROJ-1234/admin-ui"},
		},
	}

	if err := writeTaskState(path, want); err != nil {
		t.Fatalf("writeTaskState: %v", err)
	}

	got, err := readTaskState(path)
	if err != nil {
		t.Fatalf("readTaskState: %v", err)
	}

	if got.Ticket != want.Ticket {
		t.Errorf("Ticket = %q, want %q", got.Ticket, want.Ticket)
	}
	if got.Description != want.Description {
		t.Errorf("Description = %q, want %q", got.Description, want.Description)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if len(got.Repos) != len(want.Repos) {
		t.Fatalf("len(Repos) = %d, want %d", len(got.Repos), len(want.Repos))
	}
	for i := range want.Repos {
		if got.Repos[i] != want.Repos[i] {
			t.Errorf("Repos[%d] = %+v, want %+v", i, got.Repos[i], want.Repos[i])
		}
	}

	// Writing should not leave the sibling lockfile behind.
	if _, err := readTaskState(path + ".lock"); err == nil {
		t.Errorf("lockfile %s.lock should not persist after write", path)
	}
}

func TestListTasks(t *testing.T) {
	dir := t.TempDir()

	tasks := []*TaskState{
		{
			Ticket:      "PROJ-2",
			Description: "second",
			CreatedAt:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			Repos:       []TaskRepo{{Repo: "backend", Branch: "PROJ-2", WorktreePath: "/wt/PROJ-2/backend"}},
		},
		{
			Ticket:      "PROJ-1",
			Description: "first",
			CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Repos:       []TaskRepo{{Repo: "backend", Branch: "PROJ-1", WorktreePath: "/wt/PROJ-1/backend"}},
		},
	}

	for _, tk := range tasks {
		p := filepath.Join(dir, tk.Ticket+".json")
		if err := writeTaskState(p, tk); err != nil {
			t.Fatalf("writeTaskState(%s): %v", tk.Ticket, err)
		}
	}

	// A non-JSON file in the same dir should be ignored by the glob.
	got, err := listTasks(dir)
	if err != nil {
		t.Fatalf("listTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(listTasks) = %d, want 2", len(got))
	}
	// listTasks sorts by ticket.
	if got[0].Ticket != "PROJ-1" || got[1].Ticket != "PROJ-2" {
		t.Errorf("listTasks order = [%s, %s], want [PROJ-1, PROJ-2]", got[0].Ticket, got[1].Ticket)
	}
}
