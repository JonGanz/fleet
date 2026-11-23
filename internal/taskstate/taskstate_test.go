package taskstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks", "PROJ-1234.json")

	want := &State{
		Ticket:      "PROJ-1234",
		Description: "Add retry logic to payment webhook",
		CreatedAt:   time.Date(2026, 8, 2, 22, 58, 0, 0, time.UTC),
		Repos: []Repo{
			{Repo: "backend", Branch: "PROJ-1234", WorktreePath: "/home/jon/.local/state/fleet/worktrees/PROJ-1234/backend", Patches: []string{"local-env.patch"}},
			{Repo: "admin-ui", Branch: "PROJ-1234", WorktreePath: "/home/jon/.local/state/fleet/worktrees/PROJ-1234/admin-ui", Patches: []string{}},
			{Repo: "legacy", Branch: "PROJ-1234", WorktreePath: "/home/jon/.local/state/fleet/worktrees/PROJ-1234/legacy"},
		},
	}

	if err := Write(path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
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
		// reflect.DeepEqual distinguishes nil Patches (unknown, legacy) from
		// an empty list (none applied); both must survive the round trip.
		if !reflect.DeepEqual(got.Repos[i], want.Repos[i]) {
			t.Errorf("Repos[%d] = %+v, want %+v", i, got.Repos[i], want.Repos[i])
		}
	}

	// Writing should not leave the sibling lockfile behind.
	if _, err := Read(path + ".lock"); err == nil {
		t.Errorf("lockfile %s.lock should not persist after write", path)
	}
}

func TestListTasks(t *testing.T) {
	dir := t.TempDir()

	tasks := []*State{
		{
			Ticket:      "PROJ-2",
			Description: "second",
			CreatedAt:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			Repos:       []Repo{{Repo: "backend", Branch: "PROJ-2", WorktreePath: "/wt/PROJ-2/backend"}},
		},
		{
			Ticket:      "PROJ-1",
			Description: "first",
			CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Repos:       []Repo{{Repo: "backend", Branch: "PROJ-1", WorktreePath: "/wt/PROJ-1/backend"}},
		},
	}

	for _, tk := range tasks {
		p := filepath.Join(dir, tk.Ticket+".json")
		if err := Write(p, tk); err != nil {
			t.Fatalf("Write(%s): %v", tk.Ticket, err)
		}
	}

	// A non-JSON file in the same dir should be ignored by the glob.
	got, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(List) = %d, want 2", len(got))
	}
	// List sorts by ticket.
	if got[0].Ticket != "PROJ-1" || got[1].Ticket != "PROJ-2" {
		t.Errorf("List order = [%s, %s], want [PROJ-1, PROJ-2]", got[0].Ticket, got[1].Ticket)
	}
}

const stateFixture = `{
  "ticket": "PROJ-1234",
  "description": "Add retry logic to payment webhook",
  "created_at": "2026-08-02T22:58:00Z",
  "repos": [
    { "repo": "backend", "branch": "PROJ-1234", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/backend" },
    { "repo": "admin-ui", "branch": "PROJ-1234", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/admin-ui" }
  ]
}`

func TestParse(t *testing.T) {
	ts, err := Parse([]byte(stateFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
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
	if ts.Repos[0].Patches != nil {
		t.Errorf("absent patches must stay nil (unknown), got %v", ts.Repos[0].Patches)
	}
}

func TestParseCreatedAt(t *testing.T) {
	ts, err := Parse([]byte(stateFixture))
	if err != nil {
		t.Fatal(err)
	}
	if ts.CreatedAt.Year() != 2026 || ts.CreatedAt.Minute() != 58 {
		t.Errorf("CreatedAt = %v", ts.CreatedAt)
	}
	// Write stores time.Time with fractional seconds / offsets.
	if _, err := Parse([]byte(`{"ticket":"A","created_at":"2026-08-02T15:58:00.123456789-07:00","repos":[]}`)); err != nil {
		t.Errorf("RFC3339Nano with offset: %v", err)
	}
}

func TestSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "A-1.json")
	if err := Write(p, &State{Ticket: "A-1"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "{\n  \"schema_version\": 1,\n") {
		t.Errorf("written file must start with schema_version 1:\n%s", data)
	}

	// No field: read as version 1.
	if _, err := Parse([]byte(`{"ticket":"A"}`)); err != nil {
		t.Errorf("missing schema_version: %v", err)
	}
	if _, err := Parse([]byte(`{"schema_version":-1}`)); err == nil {
		t.Error("negative schema_version must be rejected")
	}

	// Newer: refused by Read with an upgrade hint, skipped by List.
	newer := filepath.Join(dir, "B-1.json")
	if err := os.WriteFile(newer, []byte(`{"schema_version": 2, "ticket": "B-1", "repos": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Read(newer)
	var tooNew *TooNewError
	if !errors.As(err, &tooNew) || tooNew.Path != newer || tooNew.Version != 2 || !strings.Contains(err.Error(), "upgrade fleet") {
		t.Errorf("Read(newer) = %v, want *TooNewError naming the file", err)
	}
	var buf bytes.Buffer
	defer cli.SetStderr(&buf)()
	got, err := List(dir)
	if err != nil || len(got) != 1 || got[0].Ticket != "A-1" {
		t.Fatalf("List = %+v, %v; want only A-1", got, err)
	}
	if !strings.Contains(buf.String(), "warning: skipping "+newer) {
		t.Errorf("List must warn about the skipped file, got %q", buf.String())
	}
}

func TestStateJSONHasNoSchemaVersion(t *testing.T) {
	data, err := json.Marshal(&State{Ticket: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "schema_version") {
		t.Errorf("State's own JSON (used in --json output) must not carry schema_version: %s", data)
	}
}

func TestReadPrefersFilename(t *testing.T) {
	var buf bytes.Buffer
	defer cli.SetStderr(&buf)()
	dir := t.TempDir()
	p := filepath.Join(dir, "REAL-1.json")
	if err := Write(p, &State{Ticket: "WRONG-1"}); err != nil {
		t.Fatal(err)
	}
	st, err := Read(p)
	if err != nil || st.Ticket != "REAL-1" {
		t.Fatalf("Read = %+v, %v", st, err)
	}
	if !strings.Contains(buf.String(), `says ticket "WRONG-1"; using "REAL-1"`) {
		t.Errorf("mismatch must be warned about, got %q", buf.String())
	}
	got, err := List(dir)
	if err != nil || len(got) != 1 || got[0].Ticket != "REAL-1" {
		t.Fatalf("List = %+v, %v", got, err)
	}
}

func TestPathValidatesTicket(t *testing.T) {
	for _, bad := range []string{"", "..", ".", "a/b", `a\b`, "../x"} {
		if _, err := Path("/state/tasks", bad); err == nil || !cli.IsUsage(err) {
			t.Errorf("Path(%q) = %v, want a usage error", bad, err)
		}
	}
	if p, err := Path("/state/tasks", "PROJ-1"); err != nil || p != "/state/tasks/PROJ-1.json" {
		t.Errorf("Path = %q, %v", p, err)
	}
}

func TestTicketFromPath(t *testing.T) {
	if got := TicketFromPath("/x/tasks/A-1.json"); got != "A-1" {
		t.Errorf("TicketFromPath = %q", got)
	}
}

func TestValidateTicket(t *testing.T) {
	good := []string{"PROJ-1234", "gh-123", "fix_thing.v2", "a", strings.Repeat("x", 100), "ünï-1", "a.b-c_d+e@f"}
	bad := []string{
		"", ".", "..", "a/b", `a\b`, "-rf", "a b", "a\tb", "a\nb", "a\x00b", "a\x7fb",
		"a<b", "a>b", "a:b", `a"b`, "a|b", "a?b", "a*b", "trailing.", "trailing ",
		strings.Repeat("x", 101), "bad\xffutf8", "../x",
	}
	for _, s := range good {
		if err := ValidateTicket(s); err != nil {
			t.Errorf("ValidateTicket(%q) = %v, want ok", s, err)
		}
	}
	for _, s := range bad {
		err := ValidateTicket(s)
		if err == nil {
			t.Errorf("ValidateTicket(%q): want error", s)
		} else if !cli.IsUsage(err) {
			t.Errorf("ValidateTicket(%q): want a usage error, got %v", s, err)
		}
	}
}
