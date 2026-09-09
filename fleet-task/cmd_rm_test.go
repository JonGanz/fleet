package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSelfHealWorktreeRemove_NilError(t *testing.T) {
	if err := selfHealWorktreeRemove(t.TempDir(), nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestSelfHealWorktreeRemove_PathGone(t *testing.T) {
	orig := errors.New("boom")
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	if err := selfHealWorktreeRemove(dir, orig); !errors.Is(err, orig) {
		t.Fatalf("expected original error surfaced, got %v", err)
	}
}

func TestSelfHealWorktreeRemove_StillRegistered(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := errors.New("boom")
	if err := selfHealWorktreeRemove(dir, orig); !errors.Is(err, orig) {
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
	if err := selfHealWorktreeRemove(dir, errors.New("directory not empty")); err != nil {
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
