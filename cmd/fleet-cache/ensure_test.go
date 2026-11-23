package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAlreadyLinkedMatchesWrittenMarker(t *testing.T) {
	target := filepath.Join(t.TempDir(), "node_modules")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	if err := writeMarker(target, "abc123"); err != nil {
		t.Fatalf("writeMarker: %v", err)
	}

	if !alreadyLinked(target, "abc123") {
		t.Error("alreadyLinked: expected true for matching hash")
	}
	if alreadyLinked(target, "different") {
		t.Error("alreadyLinked: expected false for a different hash")
	}
}

func TestAlreadyLinkedNoMarker(t *testing.T) {
	target := filepath.Join(t.TempDir(), "node_modules")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	if alreadyLinked(target, "abc123") {
		t.Error("alreadyLinked: expected false when no marker file exists")
	}
}

func TestAlreadyLinkedMissingTargetDir(t *testing.T) {
	// Target doesn't even exist yet -- reading the marker should just fail
	// closed (not already linked), not panic.
	target := filepath.Join(t.TempDir(), "node_modules")

	if alreadyLinked(target, "abc123") {
		t.Error("alreadyLinked: expected false when target dir doesn't exist")
	}
}
