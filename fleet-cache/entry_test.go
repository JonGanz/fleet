package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fakeProject(t *testing.T) (dir, lockPath, hash string) {
	t.Helper()
	dir = t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"lock":1}`), 0o644)
	lockPath, hash, err := lockfileHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, lockPath, hash
}

func fakeInstall(staging string) error {
	return os.MkdirAll(filepath.Join(staging, "node_modules", "pkg"), 0o755)
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

func TestLockfileHashMissing(t *testing.T) {
	if _, _, err := lockfileHash(t.TempDir()); err == nil {
		t.Fatal("expected error for missing lockfile")
	}
}

func TestEnsureCacheEntrySuccess(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)

	err := ensureCacheEntry(entry, hash, lock, entryOps{install: fakeInstall, remove: os.RemoveAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"package.json", "package-lock.json", "node_modules/pkg"} {
		if _, err := os.Stat(filepath.Join(entry, f)); err != nil {
			t.Errorf("entry missing %s: %v", f, err)
		}
	}
	noStaging(t, nc)

	// second call is a no-op: install must not run
	err = ensureCacheEntry(entry, hash, lock, entryOps{
		install: func(string) error { t.Error("install ran for existing entry"); return nil },
		remove:  os.RemoveAll,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCacheEntryFailureLeavesNothing(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)

	boom := errors.New("npm exploded")
	err := ensureCacheEntry(entry, hash, lock, entryOps{
		install: func(s string) error { fakeInstall(s); return boom },
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
	err = ensureCacheEntry(entry, hash, lock, entryOps{
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

func TestEnsureCacheEntryLosesRace(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)

	// While our install runs, "another process" finishes the same entry.
	err := ensureCacheEntry(entry, hash, lock, entryOps{
		install: func(staging string) error {
			if err := os.MkdirAll(filepath.Join(entry, "node_modules", "winner"), 0o755); err != nil {
				return err
			}
			return fakeInstall(staging)
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

func TestEnsureCacheEntryReplacesLegacyPartialEntry(t *testing.T) {
	_, lock, hash := fakeProject(t)
	nc := filepath.Join(t.TempDir(), "node-cache")
	entry := filepath.Join(nc, hash)
	os.MkdirAll(entry, 0o755) // dir without node_modules
	os.WriteFile(filepath.Join(entry, "junk"), nil, 0o644)

	if err := ensureCacheEntry(entry, hash, lock, entryOps{install: fakeInstall, remove: os.RemoveAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(entry, "node_modules", "pkg")); err != nil {
		t.Error("entry should have been rebuilt")
	}
	if _, err := os.Stat(filepath.Join(entry, "junk")); err == nil {
		t.Error("partial entry's junk should be gone")
	}
}

func TestStageEntryInputsDetectsLockfileChange(t *testing.T) {
	dir, lock, _ := fakeProject(t)
	_ = dir
	if err := stageEntryInputs(filepath.Join(t.TempDir(), "s"), lock, "not-the-hash"); err == nil {
		t.Fatal("expected mismatch error")
	}
}
