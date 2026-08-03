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
