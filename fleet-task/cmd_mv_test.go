package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateTicketName(t *testing.T) {
	cases := []struct {
		ticket  string
		wantErr bool
	}{
		{"PROJ-1234", false},
		{"", true},
		{".", true},
		{"..", true},
		{"a/b", true},
		{`a\b`, true},
		{"../escape", true},
	}
	for _, c := range cases {
		err := validateTicketName(c.ticket)
		if (err != nil) != c.wantErr {
			t.Errorf("validateTicketName(%q): err = %v, wantErr = %v", c.ticket, err, c.wantErr)
		}
	}
}

func TestRepoNames(t *testing.T) {
	repos := []TaskRepo{{Repo: "backend"}, {Repo: "frontend"}}
	if got, want := repoNames(repos), "backend, frontend"; got != want {
		t.Errorf("repoNames() = %q, want %q", got, want)
	}
	if got, want := repoNames(nil), ""; got != want {
		t.Errorf("repoNames(nil) = %q, want %q", got, want)
	}
}

func TestMoveTicketDirLeftovers(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "OLD-1")
	newDir := filepath.Join(root, "NEW-1")
	if err := os.MkdirAll(filepath.Join(oldDir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "notes", "todo.md"), []byte("todo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, ".env"), []byte("X=1"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := moveTicketDirLeftovers(oldDir, newDir); err != nil {
		t.Fatalf("moveTicketDirLeftovers: %v", err)
	}
	for _, p := range []string{"notes/todo.md", ".env"} {
		if _, err := os.Stat(filepath.Join(newDir, p)); err != nil {
			t.Errorf("%s not moved: %v", p, err)
		}
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old dir still exists: %v", err)
	}
}

func TestMoveTicketDirLeftoversKeepsCollisions(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "OLD-1")
	newDir := filepath.Join(root, "NEW-1")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldDir, "notes.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "notes.md"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := moveTicketDirLeftovers(oldDir, newDir); err == nil {
		t.Fatal("expected an error for the colliding entry")
	}
	if b, err := os.ReadFile(filepath.Join(oldDir, "notes.md")); err != nil || string(b) != "old" {
		t.Errorf("colliding file in old dir was not kept: %q, %v", b, err)
	}
	if b, _ := os.ReadFile(filepath.Join(newDir, "notes.md")); string(b) != "new" {
		t.Errorf("file in new dir was overwritten: %q", b)
	}
}

func TestMoveTicketDirLeftoversMissingOldDir(t *testing.T) {
	root := t.TempDir()
	if err := moveTicketDirLeftovers(filepath.Join(root, "OLD-1"), filepath.Join(root, "NEW-1")); err != nil {
		t.Errorf("missing old dir: %v", err)
	}
}
