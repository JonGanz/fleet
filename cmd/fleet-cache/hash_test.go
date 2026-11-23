package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keyProject writes a package-lock.json (and, unless npmrc is nil, an
// .npmrc) into a fresh dir and returns that dir's cache key.
func keyProject(t *testing.T, lock string, npmrc []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	if npmrc != nil {
		if err := os.WriteFile(filepath.Join(dir, ".npmrc"), npmrc, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, key, err := cacheKey(dir)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	return key
}

func TestCacheKeyMissingLockfile(t *testing.T) {
	if _, _, err := cacheKey(t.TempDir()); err == nil {
		t.Fatal("expected error for missing lockfile")
	}
}

func TestCacheKeyNpmrcChangesKey(t *testing.T) {
	const lock = `{"lock":1}`
	keys := map[string]string{
		"no .npmrc":    keyProject(t, lock, nil),
		"empty .npmrc": keyProject(t, lock, []byte{}),
		"registry A":   keyProject(t, lock, []byte("registry=https://a.example/\n")),
		"registry B":   keyProject(t, lock, []byte("registry=https://b.example/\n")),
		"token 1":      keyProject(t, lock, []byte("//a.example/:_authToken=one\n")),
		"token 2":      keyProject(t, lock, []byte("//a.example/:_authToken=two\n")),
	}
	seen := map[string]string{}
	for name, key := range keys {
		if other, dup := seen[key]; dup {
			t.Errorf("%q and %q share key %s", name, other, key)
		}
		seen[key] = name
		if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
			t.Errorf("%q: key %q is not 64 lowercase hex chars", name, key)
		}
	}
}

func TestCacheKeyRejectsNonRegularNpmrc(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644)
	os.Mkdir(filepath.Join(dir, ".npmrc"), 0o755)
	if _, _, err := cacheKey(dir); err == nil {
		t.Fatal("a directory named .npmrc should be an error")
	}
}

func TestCacheKeyStable(t *testing.T) {
	const lock = `{"lock":1}`
	npmrc := []byte("legacy-peer-deps=true\n")
	first := keyProject(t, lock, npmrc)
	// Same inputs in another dir (keys don't depend on the path)...
	if again := keyProject(t, lock, npmrc); again != first {
		t.Errorf("same inputs, different keys: %s vs %s", first, again)
	}
	// ...and a different lockfile changes it.
	if other := keyProject(t, `{"lock":2}`, npmrc); other == first {
		t.Error("different lockfile, same key")
	}
}

func TestEncodeCacheKeyGolden(t *testing.T) {
	// Pins the v2 encoding; changing it orphans every cache entry, so it
	// must come with a cacheKeyVersion bump. Precomputed via:
	// printf 'fleet-cache-key-v2\nlock:<hash>\nnpmrc:none\n' | sha256sum
	const lockHash = "2ed2faf612b56137f83e8746b8e1d9950d5607fa468467b8aca808dd7a45817e"
	const want = "e1e49b9f301523b92794c680846690c347ae9c0d46f427c244a03f567b964721"
	if got := encodeCacheKey(lockHash, noNpmrc); got != want {
		t.Fatalf("encodeCacheKey = %s, want %s", got, want)
	}
	// Never the old (v1) key, the bare lockfile hash.
	if got := keyProject(t, `{"name":"example","version":"1.0.0"}`, nil); got != want || got == lockHash {
		t.Fatalf("cacheKey = %s, want %s", got, want)
	}
}

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
