package main

import (
	"os"
	"path/filepath"
	"testing"
)

const taskStateFixture = `{
  "ticket": "PROJ-1234",
  "description": "Add retry logic to payment webhook",
  "created_at": "2026-08-02T22:58:00Z",
  "repos": [
    { "repo": "backend", "branch": "PROJ-1234", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/backend" },
    { "repo": "admin-ui", "branch": "PROJ-1234", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/admin-ui" }
  ]
}`

func TestParseTaskState(t *testing.T) {
	ts, err := parseTaskState([]byte(taskStateFixture))
	if err != nil {
		t.Fatalf("parseTaskState: %v", err)
	}
	if ts.Ticket != "PROJ-1234" {
		t.Errorf("Ticket = %q, want PROJ-1234", ts.Ticket)
	}
	if len(ts.Repos) != 2 {
		t.Fatalf("len(Repos) = %d, want 2", len(ts.Repos))
	}
	if ts.Repos[0].Repo != "backend" || ts.Repos[0].WorktreePath != "/home/jon/.local/state/fleet/worktrees/PROJ-1234/backend" {
		t.Errorf("Repos[0] = %+v", ts.Repos[0])
	}
}

func writeTaskFile(t *testing.T, dir, ticket string) {
	t.Helper()
	data := `{"ticket": "` + ticket + `", "repos": []}`
	if err := os.WriteFile(filepath.Join(dir, ticket+".json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTicketStateExplicit(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, "PROJ-1234")
	writeTaskFile(t, dir, "PROJ-5678")

	ts, err := resolveTicketState(dir, "PROJ-5678")
	if err != nil {
		t.Fatalf("resolveTicketState: %v", err)
	}
	if ts.Ticket != "PROJ-5678" {
		t.Errorf("Ticket = %q, want PROJ-5678", ts.Ticket)
	}
}

func TestResolveTicketStateExplicitMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveTicketState(dir, "PROJ-0000"); err == nil {
		t.Error("expected error for missing ticket, got nil")
	}
}

func TestResolveTicketStateSingleFallback(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, "PROJ-1234")

	ts, err := resolveTicketState(dir, "")
	if err != nil {
		t.Fatalf("resolveTicketState: %v", err)
	}
	if ts.Ticket != "PROJ-1234" {
		t.Errorf("Ticket = %q, want PROJ-1234", ts.Ticket)
	}
}

func TestResolveTicketStateNoneErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveTicketState(dir, ""); err == nil {
		t.Error("expected error when zero tasks exist, got nil")
	}
}

func TestResolveTicketStateMultipleErrors(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, "PROJ-1234")
	writeTaskFile(t, dir, "PROJ-5678")

	if _, err := resolveTicketState(dir, ""); err == nil {
		t.Error("expected error when multiple tasks exist and no --ticket given, got nil")
	}
}

func TestTicketsFromFiles(t *testing.T) {
	got := ticketsFromFiles([]string{"/x/tasks/A-1.json", "/x/tasks/B-2.json"})
	if len(got) != 2 || got[0] != "A-1" || got[1] != "B-2" {
		t.Errorf("ticketsFromFiles = %v", got)
	}
}

func TestParseTaskStateCreatedAt(t *testing.T) {
	ts, err := parseTaskState([]byte(taskStateFixture))
	if err != nil {
		t.Fatal(err)
	}
	if ts.CreatedAt.Year() != 2026 || ts.CreatedAt.Minute() != 58 {
		t.Errorf("CreatedAt = %v", ts.CreatedAt)
	}
	// fleet-task writes time.Time with fractional seconds / offsets.
	if _, err := parseTaskState([]byte(`{"ticket":"A","created_at":"2026-08-02T15:58:00.123456789-07:00","repos":[]}`)); err != nil {
		t.Errorf("RFC3339Nano with offset: %v", err)
	}
}
