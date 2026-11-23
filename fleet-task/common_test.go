package main

import (
	"path/filepath"
	"strings"
	"testing"
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

func TestValidateTicketNameExtended(t *testing.T) {
	good := []string{"PROJ-1234", "gh-123", "fix_thing.v2", "a", strings.Repeat("x", 100), "ünï-1", "a.b-c_d+e@f"}
	bad := []string{
		"", ".", "..", "a/b", `a\b`, "-rf", "a b", "a\tb", "a\nb", "a\x00b", "a\x7fb",
		"a<b", "a>b", "a:b", `a"b`, "a|b", "a?b", "a*b", "trailing.", "trailing ",
		strings.Repeat("x", 101), "bad\xffutf8",
	}
	for _, s := range good {
		if err := validateTicketName(s); err != nil {
			t.Errorf("validateTicketName(%q) = %v, want ok", s, err)
		}
	}
	for _, s := range bad {
		err := validateTicketName(s)
		if err == nil {
			t.Errorf("validateTicketName(%q): want error", s)
		} else if !isUsageError(err) {
			t.Errorf("validateTicketName(%q): want a usage error, got %v", s, err)
		}
	}
}

func TestLockAndReadTaskUnknownIsUsageError(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	_, _, _, err := lockAndReadTask("NOPE-1")
	if err == nil || !isUsageError(err) || !strings.Contains(err.Error(), "no task found for ticket NOPE-1") {
		t.Errorf("want usage error naming the ticket, got %v", err)
	}
}

func TestLockAndReadTaskUsesFilenameTicket(t *testing.T) {
	t.Setenv("FLEET_STATE_DIR", t.TempDir())
	tf, err := taskFile("REAL-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTaskState(tf, &TaskState{Ticket: "OTHER-9"}); err != nil {
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
