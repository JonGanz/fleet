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

func TestAddWorktreeWithFallback(t *testing.T) {
	ok := func() error { return nil }
	failNew := func() error { return errString("bad ref origin/mian") }
	failExisting := func() error { return errString("invalid reference: T-1") }

	if err := addWorktreeWithFallback("T-1", ok, failExisting); err != nil {
		t.Errorf("first attempt ok: %v", err)
	}
	if err := addWorktreeWithFallback("T-1", failNew, ok); err != nil {
		t.Errorf("fallback ok: %v", err)
	}
	err := addWorktreeWithFallback("T-1", failNew, failExisting)
	if err == nil || !strings.Contains(err.Error(), "origin/mian") || !strings.Contains(err.Error(), "invalid reference") {
		t.Errorf("both failing must report both errors: %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
