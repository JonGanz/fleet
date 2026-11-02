package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
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

func TestReferencedHashesMissingRootErrors(t *testing.T) {
	if _, err := referencedHashes([]string{filepath.Join(t.TempDir(), "typo")}); err == nil {
		t.Fatal("missing root should error")
	}
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	if _, err := referencedHashes([]string{f}); err == nil {
		t.Fatal("non-directory root should error")
	}
}

func TestReferencedHashesFollowsSymlinks(t *testing.T) {
	real := t.TempDir()
	os.MkdirAll(filepath.Join(real, "wt"), 0o755)
	os.WriteFile(filepath.Join(real, "wt", "package-lock.json"), []byte("{}"), 0o644)
	want, _ := hashFile(filepath.Join(real, "wt", "package-lock.json"))

	// symlinked root
	link := filepath.Join(t.TempDir(), "root")
	os.Symlink(real, link)
	got, err := referencedHashes([]string{link})
	if err != nil || !got[want] {
		t.Fatalf("symlinked root: %v %v", got, err)
	}

	// symlinked directory inside a root, plus a cycle back to the root
	root := t.TempDir()
	os.Symlink(filepath.Join(real, "wt"), filepath.Join(root, "ticket"))
	os.Symlink(root, filepath.Join(root, "loop"))
	got, err = referencedHashes([]string{root})
	if err != nil || !got[want] {
		t.Fatalf("symlinked subdir: %v %v", got, err)
	}
}

func fakeCache(t *testing.T) (nodeCache string, hashes []string) {
	t.Helper()
	nodeCache = filepath.Join(t.TempDir(), "node-cache")
	for _, h := range []string{"aaa", "bbb"} {
		if err := os.MkdirAll(filepath.Join(nodeCache, h, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h)
	}
	return
}

func identity(p string) (string, error) { return p, nil }

func TestGCStaleMissingRootDeletesNothing(t *testing.T) {
	nc, _ := fakeCache(t)
	removed := 0
	_, err := gcStale(nc, []string{filepath.Join(t.TempDir(), "typo")}, true, time.Now(),
		func(string) error { removed++; return nil }, identity)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
	if removed != 0 {
		t.Fatalf("removed %d entries despite error", removed)
	}
	if _, err := os.Stat(filepath.Join(nc, "aaa")); err != nil {
		t.Fatalf("entry should still exist: %v", err)
	}
}

func TestGCStaleDryRunVsForce(t *testing.T) {
	nc, _ := fakeCache(t)
	root := t.TempDir()
	var removed []string
	rm := func(p string) error { removed = append(removed, filepath.Base(p)); return os.RemoveAll(p) }

	// no force: report only
	r, err := gcStale(nc, []string{root}, false, time.Now(), rm, identity)
	if err != nil || !r.DryRun || len(r.Stale) != 2 || len(removed) != 0 {
		t.Fatalf("dry run: %+v %v %v", r, err, removed)
	}
	// force but no roots: still report only
	r, err = gcStale(nc, nil, true, time.Now(), rm, identity)
	if err != nil || !r.DryRun || len(removed) != 0 {
		t.Fatalf("no roots: %+v %v %v", r, err, removed)
	}
	// force with roots: deletes
	r, err = gcStale(nc, []string{root}, true, time.Now(), rm, identity)
	if err != nil || r.DryRun || len(removed) != 2 || !r.Stale[0].Removed {
		t.Fatalf("force: %+v %v %v", r, err, removed)
	}
}

func TestGCStaleStagingDirs(t *testing.T) {
	nc, _ := fakeCache(t)
	fresh := filepath.Join(nc, "ccc.tmp-111")
	old := filepath.Join(nc, "ddd.tmp-222")
	os.MkdirAll(fresh, 0o755)
	os.MkdirAll(old, 0o755)
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(old, past, past)

	// everything referenced except staging dirs
	r, err := gcStale(nc, []string{t.TempDir()}, false, time.Now(), os.RemoveAll, identity)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, it := range r.Stale {
		kinds = append(kinds, it.Kind+":"+it.Name)
	}
	if !reflect.DeepEqual(kinds, []string{"entry:aaa", "entry:bbb", "staging:ddd.tmp-222"}) {
		t.Fatalf("stale = %v (fresh staging dir must not be flagged, old one must)", kinds)
	}
	entries, _ := unreferencedEntries(nc, map[string]bool{"aaa": true, "bbb": true})
	if len(entries) != 0 {
		t.Fatalf("staging dirs must not count as entries: %v", entries)
	}
}

func TestPrintGCReportJSON(t *testing.T) {
	r := &gcReport{NodeCache: "/nc", DryRun: true, Stale: []gcItem{}}
	var buf bytes.Buffer
	if err := printGCReport(&buf, r, true, "", true); err != nil {
		t.Fatal(err)
	}
	var back gcReport
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || !back.DryRun || back.NodeCache != "/nc" {
		t.Fatalf("bad json %q: %v", buf.String(), err)
	}
	if !strings.Contains(buf.String(), `"stale": []`) {
		t.Errorf("empty stale should be [] not null: %s", buf.String())
	}
}
