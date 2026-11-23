package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/taskstate"
)

func TestTaskFileValidatesTicket(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	for _, bad := range []string{"", "..", ".", "a/b", `a\b`, "../x"} {
		if _, err := taskFile(bad); err == nil {
			t.Errorf("taskFile(%q): expected an error", bad)
		}
	}
	p, err := taskFile("PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "PROJ-1.json" || !strings.Contains(p, "tasks") {
		t.Errorf("taskFile = %q", p)
	}
}

func TestLockAndReadTaskUnknownIsUsageError(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	_, _, _, err := lockAndReadTask("NOPE-1")
	if err == nil || !cli.IsUsage(err) || !strings.Contains(err.Error(), "no task found for ticket NOPE-1") {
		t.Errorf("want usage error naming the ticket, got %v", err)
	}
}

func TestLockAndReadTaskUsesFilenameTicket(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	tf, err := taskFile("REAL-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := taskstate.Write(tf, &taskstate.State{Ticket: "OTHER-9"}); err != nil {
		t.Fatal(err)
	}
	_, st, unlock, err := lockAndReadTask("REAL-1")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if st.Ticket != "REAL-1" {
		t.Errorf("st.Ticket = %q, want filename stem", st.Ticket)
	}
	if _, _, _, err := lockAndReadTask("REAL-1"); err == nil {
		t.Error("second lockAndReadTask must fail while the first holds the lock")
	}
}

func TestLoadReposConfigOptionalInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "repos.yaml")
	if err := os.WriteFile(p, []byte("repos:\n  - name: a\n    runtime: macos\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// rm/mv's optional loader degrades to nil instead of failing.
	t.Setenv("FLEET_REPOS_FILE", p)
	if cfg := loadReposConfigOptional(); cfg != nil {
		t.Error("loadReposConfigOptional should return nil for an invalid config")
	}
}
