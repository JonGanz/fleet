package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSelfHealWorktreeRemove_NilError(t *testing.T) {
	if err := selfHealWorktreeRemove(t.TempDir(), nil, noPrune); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func noPrune() error { return nil }

func TestSelfHealWorktreeRemove_PathGone(t *testing.T) {
	// Directory deleted by hand: git's remove fails, but there's nothing
	// left to remove, so it counts as removed (after pruning).
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	pruned := false
	err := selfHealWorktreeRemove(dir, errors.New("boom"), func() error { pruned = true; return nil })
	if err != nil {
		t.Fatalf("expected nil for an already-missing worktree, got %v", err)
	}
	if !pruned {
		t.Error("expected prune to be called")
	}
}

func TestSelfHealWorktreeRemove_PathGonePruneFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	err := selfHealWorktreeRemove(dir, errors.New("boom"), func() error { return errors.New("prune failed") })
	if err != nil {
		t.Fatalf("a failed prune is only a warning, got %v", err)
	}
}

func TestSelfHealWorktreeRemove_StillRegistered(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := errors.New("boom")
	if err := selfHealWorktreeRemove(dir, orig, noPrune); !errors.Is(err, orig) {
		t.Fatalf("expected original error surfaced, got %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory should not have been swept: %v", err)
	}
}

func TestSelfHealWorktreeRemove_LeftoverCruft(t *testing.T) {
	dir := t.TempDir()
	cruft := filepath.Join(dir, ".vite", "deps_temp_abc")
	if err := os.MkdirAll(cruft, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cruft, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := selfHealWorktreeRemove(dir, errors.New("directory not empty"), noPrune); err != nil {
		t.Fatalf("expected self-heal to succeed, got %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected leftover directory to be swept, stat err = %v", err)
	}
}

func TestWorktreeAdminGone(t *testing.T) {
	dir := t.TempDir()
	if !worktreeAdminGone(dir) {
		t.Fatal("expected true: no .git entry present")
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if worktreeAdminGone(dir) {
		t.Fatal("expected false: .git entry present")
	}
}

func TestRemoveDirIfEmpty(t *testing.T) {
	root := t.TempDir()

	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	removed, left, err := removeDirIfEmpty(empty)
	if err != nil || !removed || len(left) != 0 {
		t.Errorf("empty dir: removed=%v left=%v err=%v", removed, left, err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Errorf("empty dir should be gone: %v", err)
	}

	full := filepath.Join(root, "full")
	if err := os.MkdirAll(filepath.Join(full, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, ".env"), []byte("X=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, left, err = removeDirIfEmpty(full)
	if err != nil || removed {
		t.Errorf("non-empty dir: removed=%v err=%v", removed, err)
	}
	if len(left) != 2 {
		t.Errorf("leftovers = %v, want 2 entries", left)
	}
	if _, err := os.Stat(filepath.Join(full, ".env")); err != nil {
		t.Errorf("user file must survive: %v", err)
	}

	removed, left, err = removeDirIfEmpty(filepath.Join(root, "missing"))
	if err != nil || removed || left != nil {
		t.Errorf("missing dir: removed=%v left=%v err=%v", removed, left, err)
	}
}
