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

func TestAlreadyMoved(t *testing.T) {
	root := t.TempDir()
	oldP := filepath.Join(root, "old")
	newP := filepath.Join(root, "new")

	if alreadyMoved(oldP, newP) {
		t.Error("neither exists: not moved")
	}
	if !alreadyMoved(newP, newP) {
		t.Error("state path already equals destination: moved")
	}
	if err := os.Mkdir(oldP, 0o755); err != nil {
		t.Fatal(err)
	}
	if alreadyMoved(oldP, newP) {
		t.Error("source still exists: not moved")
	}
	if err := os.Mkdir(newP, 0o755); err != nil {
		t.Fatal(err)
	}
	if alreadyMoved(oldP, newP) {
		t.Error("both exist: ambiguous, must not be treated as moved")
	}
	if err := os.Remove(oldP); err != nil {
		t.Fatal(err)
	}
	if !alreadyMoved(oldP, newP) {
		t.Error("source gone, destination present: moved")
	}
}

func TestCheckNewTicketDir(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "OLD-1")
	newDir := filepath.Join(root, "NEW-1")
	repos := []TaskRepo{
		{Repo: "backend", WorktreePath: filepath.Join(oldDir, "backend")},
		{Repo: "ui", WorktreePath: filepath.Join(oldDir, "ui")},
	}

	// Missing: fine.
	if err := checkNewTicketDir(newDir, repos); err != nil {
		t.Errorf("missing dir: %v", err)
	}

	// Empty: fine (mv creates the dir before the first move).
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkNewTicketDir(newDir, repos); err != nil {
		t.Errorf("empty dir: %v", err)
	}

	// backend already moved (source gone, destination present): fine.
	if err := os.MkdirAll(filepath.Join(newDir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkNewTicketDir(newDir, repos); err != nil {
		t.Errorf("resumable dir: %v", err)
	}

	// Unrelated entry: refused.
	if err := os.WriteFile(filepath.Join(newDir, "stranger.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkNewTicketDir(newDir, repos); err == nil {
		t.Error("unrelated entry should be refused")
	}
	os.Remove(filepath.Join(newDir, "stranger.txt"))

	// A repo-named dir whose source still exists is someone else's: refused.
	if err := os.MkdirAll(filepath.Join(oldDir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(newDir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkNewTicketDir(newDir, repos); err == nil {
		t.Error("collision with a not-yet-moved repo should be refused")
	}
}
