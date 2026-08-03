package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestReferencedHashesAndUnreferencedEntries(t *testing.T) {
	// Fake cache root with three entries: two that will be "referenced" by
	// lockfiles under a fake worktree root, and one stale entry.
	cacheRootDir := t.TempDir()
	nodeCache := filepath.Join(cacheRootDir, "node-cache")
	if err := os.MkdirAll(nodeCache, 0o755); err != nil {
		t.Fatalf("mkdir node-cache: %v", err)
	}

	lockA := []byte(`{"name":"a"}`)
	lockB := []byte(`{"name":"b"}`)
	lockStale := []byte(`{"name":"stale"}`)

	hashA, err := writeAndHash(t, lockA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := writeAndHash(t, lockB)
	if err != nil {
		t.Fatal(err)
	}
	hashStale, err := writeAndHash(t, lockStale)
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range []string{hashA, hashB, hashStale} {
		if err := os.MkdirAll(filepath.Join(nodeCache, h, "node_modules"), 0o755); err != nil {
			t.Fatalf("mkdir cache entry %s: %v", h, err)
		}
	}

	// Fake worktree roots referencing lockA and lockB, but not lockStale.
	worktrees := t.TempDir()
	repoA := filepath.Join(worktrees, "TICKET-1", "repoA")
	repoB := filepath.Join(worktrees, "TICKET-2", "repoB")
	if err := os.MkdirAll(repoA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repoB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoA, "package-lock.json"), lockA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoB, "package-lock.json"), lockB, 0o644); err != nil {
		t.Fatal(err)
	}
	// Also drop a node_modules dir with an unrelated package-lock.json in it
	// to confirm referencedHashes skips node_modules subtrees.
	nm := filepath.Join(repoA, "node_modules", "somedep")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nm, "package-lock.json"), []byte(`{"name":"ignored"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	referenced, err := referencedHashes([]string{worktrees})
	if err != nil {
		t.Fatalf("referencedHashes: %v", err)
	}
	if !referenced[hashA] || !referenced[hashB] {
		t.Fatalf("expected hashA and hashB referenced, got %v", referenced)
	}
	if referenced[hashStale] {
		t.Fatalf("hashStale should not be referenced")
	}

	stale, err := unreferencedEntries(nodeCache, referenced)
	if err != nil {
		t.Fatalf("unreferencedEntries: %v", err)
	}
	sort.Strings(stale)
	if len(stale) != 1 || stale[0] != hashStale {
		t.Fatalf("unreferencedEntries = %v, want [%s]", stale, hashStale)
	}
}

func TestUnreferencedEntriesEmptyCache(t *testing.T) {
	dir := t.TempDir()
	stale, err := unreferencedEntries(filepath.Join(dir, "does-not-exist"), map[string]bool{})
	if err != nil {
		t.Fatalf("unreferencedEntries: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected no entries for missing cache dir, got %v", stale)
	}
}

func writeAndHash(t *testing.T, content []byte) (string, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "lock-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return "", err
	}
	return hashFile(f.Name())
}
