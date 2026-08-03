package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package-lock.json")
	content := []byte(`{"name":"example","version":"1.0.0"}`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Precomputed via: printf '%s' '<content>' | sha256sum
	const want = "2ed2faf612b56137f83e8746b8e1d9950d5607fa468467b8aca808dd7a45817e"

	got, err := hashFile(path)
	if err != nil {
		t.Fatalf("hashFile: %v", err)
	}
	if got != want {
		t.Fatalf("hashFile(%s) = %s, want %s", path, got, want)
	}
}

func TestHashFileMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := hashFile(filepath.Join(dir, "does-not-exist.json")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
