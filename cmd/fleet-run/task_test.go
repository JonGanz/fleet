package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/taskstate"
)

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
	_, err := resolveTicketState(dir, "PROJ-0000")
	if err == nil {
		t.Fatal("expected error for missing ticket, got nil")
	}
	// An unknown ticket is a usage error (exit 2) per the contract.
	if cli.ExitCode(err) != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (%v)", cli.ExitCode(err), cli.ExitUsage, err)
	}
}

func TestResolveTicketStateFilenameWins(t *testing.T) {
	var stderr bytes.Buffer
	defer cli.SetStderr(&stderr)()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "REAL-1.json"), []byte(`{"ticket": "OTHER-9", "repos": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ticket := range []string{"REAL-1", ""} {
		ts, err := resolveTicketState(dir, ticket)
		if err != nil || ts.Ticket != "REAL-1" {
			t.Errorf("resolveTicketState(%q) = %+v, %v; want ticket from filename", ticket, ts, err)
		}
	}
	if !strings.Contains(stderr.String(), `says ticket "OTHER-9"; using "REAL-1"`) {
		t.Errorf("mismatch not warned about: %q", stderr.String())
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

func TestResolveTicketStateNoneErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveTicketState(dir, ""); err == nil {
		t.Error("expected error when zero tasks exist, got nil")
	}
}

func TestResolveTicketStateRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "A-1.json"), []byte(`{"schema_version": 99, "ticket": "A-1", "repos": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ticket := range []string{"A-1", ""} {
		_, err := resolveTicketState(dir, ticket)
		var tooNew *taskstate.TooNewError
		if !errors.As(err, &tooNew) {
			t.Errorf("resolveTicketState(%q) = %v, want *taskstate.TooNewError", ticket, err)
		}
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

func TestResolveTicketStateValidatesTicket(t *testing.T) {
	for _, bad := range []string{".", "..", "a/b", `a\b`, "../x", "-x", "a b"} {
		if _, err := resolveTicketState(t.TempDir(), bad); !isUsage(err) {
			t.Errorf("resolveTicketState(%q) = %v, want usage error", bad, err)
		}
	}
}

func TestTicketsFromFiles(t *testing.T) {
	got := ticketsFromFiles([]string{"/x/tasks/A-1.json", "/x/tasks/B-2.json"})
	if len(got) != 2 || got[0] != "A-1" || got[1] != "B-2" {
		t.Errorf("ticketsFromFiles = %v", got)
	}
}

func writeTaskFile(t *testing.T, dir, ticket string) {
	t.Helper()
	data := `{"ticket": "` + ticket + `", "repos": []}`
	if err := os.WriteFile(filepath.Join(dir, ticket+".json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}
