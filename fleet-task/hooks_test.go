package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverHooks(t *testing.T) {
	dir := t.TempDir()

	mustWrite := func(name string, mode os.FileMode) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	// Executables, deliberately not in sorted creation order.
	mustWrite("20-second.sh", 0o755)
	mustWrite("10-first.sh", 0o755)
	mustWrite("30-third.sh", 0o755)
	// Non-executable file: should be skipped.
	mustWrite("05-not-executable.sh", 0o644)

	// Subdirectory: should be skipped even if "executable".
	if err := os.Mkdir(filepath.Join(dir, "01-a-directory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	hooks, err := discoverHooks(dir)
	if err != nil {
		t.Fatalf("discoverHooks: %v", err)
	}

	want := []string{
		filepath.Join(dir, "10-first.sh"),
		filepath.Join(dir, "20-second.sh"),
		filepath.Join(dir, "30-third.sh"),
	}
	if len(hooks) != len(want) {
		t.Fatalf("discoverHooks = %v, want %v", hooks, want)
	}
	for i := range want {
		if hooks[i] != want[i] {
			t.Errorf("hooks[%d] = %q, want %q", i, hooks[i], want[i])
		}
	}
}

func TestDiscoverHooksMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	hooks, err := discoverHooks(dir)
	if err != nil {
		t.Fatalf("discoverHooks on missing dir should not error, got %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("discoverHooks on missing dir = %v, want empty", hooks)
	}
}
