package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeInstall(_ context.Context, staging string) error {
	return os.MkdirAll(filepath.Join(staging, "node_modules", "pkg"), 0o755)
}

func fakeProject(t *testing.T) (dir, lockPath, hash string) {
	t.Helper()
	dir = t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"lock":1}`), 0o644)
	lockPath, hash, err := cacheKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, lockPath, hash
}

func noStaging(t *testing.T, nodeCache string) {
	t.Helper()
	items, _ := os.ReadDir(nodeCache)
	for _, it := range items {
		if isStagingName(it.Name()) {
			t.Errorf("staging dir left behind: %s", it.Name())
		}
	}
}

func TestDeclaresWorkspaces(t *testing.T) {
	cases := []struct {
		json    string
		want    bool
		wantErr bool
	}{
		{`{"name":"x"}`, false, false},
		{`{"workspaces":null}`, false, false},
		{`{"workspaces":[ ]}`, false, false},
		{`{"workspaces":{}}`, false, false},
		{`{"workspaces":["packages/*"]}`, true, false},
		{`{"workspaces":{"packages":["a"]}}`, true, false},
		{`{"name":`, false, true},
		{`["not","an","object"]`, false, true},
		{``, false, true},
	}
	for _, c := range cases {
		got, err := declaresWorkspaces([]byte(c.json))
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%q: got %v, %v; want %v, err=%v", c.json, got, err, c.want, c.wantErr)
		}
	}
}

func TestEnsureCacheEntryFailureLeavesNothing(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)

	boom := errors.New("npm exploded")
	err := ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{
		install: func(ctx context.Context, s string) error { fakeInstall(ctx, s); return boom },
		remove:  os.RemoveAll,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Error("entry must not exist after failed install")
	}
	noStaging(t, nc)

	// finalize failure also leaves no entry
	err = ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{
		install:  fakeInstall,
		finalize: func(string) error { return boom },
		remove:   os.RemoveAll,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Error("entry must not exist after failed finalize")
	}
	noStaging(t, nc)
}

func TestEnsureCacheEntryInterruptedCleansUp(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)
	ctx, cancel := context.WithCancel(context.Background())

	// The "signal" arrives while npm ci runs; install itself succeeds (as npm
	// might, racing the signal), but the entry must still not be committed.
	err := ensureCacheEntry(ctx, entry, hash, lock, entryOps{
		install: func(ctx context.Context, staging string) error {
			cancel()
			return fakeInstall(ctx, staging)
		},
		remove: os.RemoveAll,
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("err = %v, want interrupted", err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Error("entry must not exist after an interrupt")
	}
	noStaging(t, nc)
}

func TestEnsureCacheEntryLosesRace(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)

	// While our install runs, "another process" finishes the same entry.
	err := ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{
		install: func(ctx context.Context, staging string) error {
			if err := os.MkdirAll(filepath.Join(entry, "node_modules", "winner"), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(entry, entryCompleteMarker), nil, 0o644); err != nil {
				return err
			}
			return fakeInstall(ctx, staging)
		},
		remove: os.RemoveAll,
	})
	if err != nil {
		t.Fatalf("losing the race must not be an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(entry, "node_modules", "winner")); err != nil {
		t.Error("winner's entry must be left intact")
	}
	if _, err := os.Stat(filepath.Join(entry, "node_modules", "pkg")); err == nil {
		t.Error("loser's content must not be merged into the winner's entry")
	}
	noStaging(t, nc)
}

func TestEnsureCacheEntryMalformedPackageJSON(t *testing.T) {
	dir, lock, hash := fakeProject(t)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":`), 0o644)
	nc := filepath.Join(t.TempDir(), "node-cache")

	err := ensureCacheEntry(context.Background(), filepath.Join(nc, hash), hash, lock, entryOps{install: fakeInstall, remove: os.RemoveAll})
	if err == nil || !strings.Contains(err.Error(), "package.json") {
		t.Fatalf("err = %v, want a clear package.json error", err)
	}
	noStaging(t, nc)
}

func TestEnsureCacheEntryNpmrcOnlyDuringInstall(t *testing.T) {
	for _, installFails := range []bool{false, true} {
		dir, lock, _ := fakeProject(t)
		os.WriteFile(filepath.Join(dir, ".npmrc"), []byte("//registry/:_authToken=secret\n"), 0o644)
		_, hash, err := cacheKey(dir) // the .npmrc is part of the key
		if err != nil {
			t.Fatal(err)
		}
		nc := filepath.Join(t.TempDir(), "node-cache")
		entry := filepath.Join(nc, hash)

		var sawMode os.FileMode
		var sawContent string
		boom := errors.New("npm exploded")
		err = ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{
			install: func(ctx context.Context, staging string) error {
				p := filepath.Join(staging, ".npmrc")
				if fi, err := os.Stat(p); err == nil {
					sawMode = fi.Mode().Perm()
				}
				data, _ := os.ReadFile(p)
				sawContent = string(data)
				if installFails {
					return boom
				}
				return fakeInstall(ctx, staging)
			},
			remove: os.RemoveAll,
		})
		if installFails != (err != nil) {
			t.Fatalf("installFails=%v: err = %v", installFails, err)
		}
		if !strings.Contains(sawContent, "secret") {
			t.Errorf("installFails=%v: .npmrc not staged for install (got %q)", installFails, sawContent)
		}
		if sawMode != 0o600 {
			t.Errorf("installFails=%v: staged .npmrc mode = %o, want 600", installFails, sawMode)
		}
		if _, err := os.Lstat(filepath.Join(entry, ".npmrc")); err == nil {
			t.Errorf("installFails=%v: .npmrc must not persist in the entry", installFails)
		}
		noStaging(t, nc)
	}
}

func TestEnsureCacheEntryRejectsWorkspaces(t *testing.T) {
	dir, lock, hash := fakeProject(t)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"workspaces":["packages/*"]}`), 0o644)
	nc := filepath.Join(t.TempDir(), "node-cache")

	err := ensureCacheEntry(context.Background(), filepath.Join(nc, hash), hash, lock, entryOps{
		install: func(context.Context, string) error { t.Error("install must not run for workspaces"); return nil },
		remove:  os.RemoveAll,
	})
	if err == nil || !strings.Contains(err.Error(), "workspaces") || !strings.Contains(err.Error(), "npm ci") {
		t.Fatalf("err = %v, want a workspaces error pointing at npm ci", err)
	}
	noStaging(t, nc)
}

func TestEnsureCacheEntryReplacesLegacyEntryWithoutMarker(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)
	// legacy entry: node_modules present (maybe partial), no completion marker
	os.MkdirAll(filepath.Join(entry, "node_modules", "partial"), 0o755)

	if err := ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{install: fakeInstall, remove: os.RemoveAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(entry, "node_modules", "partial")); err == nil {
		t.Error("legacy partial content must be discarded")
	}
	for _, f := range []string{"node_modules/pkg", entryCompleteMarker} {
		if _, err := os.Stat(filepath.Join(entry, f)); err != nil {
			t.Errorf("rebuilt entry missing %s: %v", f, err)
		}
	}
	noStaging(t, nc)
}

func TestEnsureCacheEntryReplacesLegacyPartialEntry(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)
	os.MkdirAll(entry, 0o755) // dir without node_modules
	os.WriteFile(filepath.Join(entry, "junk"), nil, 0o644)

	if err := ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{install: fakeInstall, remove: os.RemoveAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(entry, "node_modules", "pkg")); err != nil {
		t.Error("entry should have been rebuilt")
	}
	if _, err := os.Stat(filepath.Join(entry, "junk")); err == nil {
		t.Error("partial entry's junk should be gone")
	}
}

func TestEnsureCacheEntrySuccess(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)

	err := ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{install: fakeInstall, remove: os.RemoveAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"package.json", "package-lock.json", "node_modules/pkg", entryCompleteMarker} {
		if _, err := os.Stat(filepath.Join(entry, f)); err != nil {
			t.Errorf("entry missing %s: %v", f, err)
		}
	}
	noStaging(t, nc)

	// second call is a no-op: install must not run
	err = ensureCacheEntry(context.Background(), entry, hash, lock, entryOps{
		install: func(context.Context, string) error { t.Error("install ran for existing entry"); return nil },
		remove:  os.RemoveAll,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStageEntryInputsDetectsLockfileChange(t *testing.T) {
	dir, lock, _ := fakeProject(t)
	_ = dir
	if err := stageEntryInputs(filepath.Join(t.TempDir(), "s"), lock, "not-the-hash"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestStageEntryInputsDetectsNpmrcChange(t *testing.T) {
	dir, lock, key := fakeProject(t)
	// .npmrc appears after the key was computed (without one)
	os.WriteFile(filepath.Join(dir, ".npmrc"), []byte("registry=https://other.example/\n"), 0o600)
	err := stageEntryInputs(filepath.Join(t.TempDir(), "s"), lock, key)
	if err == nil || !strings.Contains(err.Error(), ".npmrc") {
		t.Fatalf("err = %v, want a changed-while-staging error", err)
	}
}

func TestStageNpmrcRejectsNonRegularFile(t *testing.T) {
	src := t.TempDir()
	os.Mkdir(filepath.Join(src, ".npmrc"), 0o755)
	if err := stageNpmrc(src, t.TempDir()); err == nil {
		t.Fatal("a directory named .npmrc should be an error")
	}
	if err := stageNpmrc(t.TempDir(), t.TempDir()); err != nil {
		t.Fatalf("no .npmrc should be fine: %v", err)
	}
}
