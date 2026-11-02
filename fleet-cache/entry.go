package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stagingInfix separates a cache entry's hash from its staging suffix:
// <hash>.tmp-<pid>. Entries are built in a staging dir and renamed into
// place, so an entry directory only ever exists once it is complete.
const stagingInfix = ".tmp-"

// stagingMaxAge is how old a staging dir must be before gc flags it as
// abandoned (a live `ensure` is never expected to take this long).
const stagingMaxAge = 24 * time.Hour

// logf writes fleet-cache's own progress/status output to stderr, keeping
// stdout free for machine-readable output and for callers (fleet-task)
// that interleave several tools' output.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// lockfileHash returns <dir>/package-lock.json's path and sha256.
func lockfileHash(dir string) (lockPath, hash string, err error) {
	lockPath = filepath.Join(dir, "package-lock.json")
	if _, err := os.Stat(lockPath); err != nil {
		return "", "", fmt.Errorf("no package-lock.json in %s: %w", dir, err)
	}
	hash, err = hashFile(lockPath)
	if err != nil {
		return "", "", fmt.Errorf("hashing %s: %w", lockPath, err)
	}
	return lockPath, hash, nil
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// stageEntryInputs creates stagingDir and copies package-lock.json and
// package.json (taken from lockPath's directory) into it.
//
// `npm ci` requires both files in its working directory (it validates the
// lockfile against the manifest) -- copying the lockfile alone fails with
// an ENOENT reading package.json. The copied lockfile is re-hashed to make
// sure it still matches wantHash (it could have changed since it was
// hashed), so an entry is never filed under the wrong hash.
func stageEntryInputs(stagingDir, lockPath, wantHash string) error {
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("creating cache staging dir %s: %w", stagingDir, err)
	}
	stagedLock := filepath.Join(stagingDir, "package-lock.json")
	if err := copyFile(lockPath, stagedLock); err != nil {
		return fmt.Errorf("copying package-lock.json into cache: %w", err)
	}
	if got, err := hashFile(stagedLock); err != nil || got != wantHash {
		return fmt.Errorf("package-lock.json changed while staging the cache entry; rerun")
	}
	pkgPath := filepath.Join(filepath.Dir(lockPath), "package.json")
	if err := copyFile(pkgPath, filepath.Join(stagingDir, "package.json")); err != nil {
		return fmt.Errorf("copying package.json into cache: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// stagingPath returns this process's staging dir for entry.
func stagingPath(entry string) string {
	return fmt.Sprintf("%s%s%d", entry, stagingInfix, os.Getpid())
}

func isStagingName(name string) bool {
	return strings.Contains(name, stagingInfix)
}

// entryOps are the platform-specific pieces of building a cache entry.
type entryOps struct {
	// install runs `npm ci` inside stagingDir (a WSL/Linux-form path).
	install func(stagingDir string) error
	// finalize, if non-nil, runs after a successful install (e.g.
	// lockDownPermissions on the Linux path).
	finalize func(stagingDir string) error
	// remove deletes a directory tree (os.RemoveAll, or a native Windows
	// bulk delete).
	remove func(dir string) error
}

// ensureCacheEntry makes sure the cache entry at entry (<node-cache>/<hash>)
// exists and is complete. The entry is built in a unique staging dir
// (<entry>.tmp-<pid>) and renamed into place atomically, so:
//   - an entry directory only exists once fully built (a killed `npm ci`
//     leaves only a staging dir, never a reusable partial entry);
//   - concurrent ensures of the same hash can't corrupt each other: the
//     loser's rename fails because the winner's entry exists, and it
//     discards its own staging dir and uses the winner's;
//   - the staging dir is removed on any failure.
func ensureCacheEntry(entry, hash, lockPath string, ops entryOps) error {
	nodeModules := filepath.Join(entry, "node_modules")
	if _, err := os.Stat(nodeModules); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking cache entry %s: %w", entry, err)
	}

	// An entry dir without node_modules can only be a partial entry from
	// before staging existed; it can't be trusted, and rename can't replace it.
	if _, err := os.Lstat(entry); err == nil {
		logf("removing incomplete cache entry %s", entry)
		if err := ops.remove(entry); err != nil {
			return fmt.Errorf("removing incomplete cache entry %s: %w", entry, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	staging := stagingPath(entry)
	cleanup := func() {
		if _, err := os.Lstat(staging); err == nil {
			_ = ops.remove(staging)
		}
	}
	cleanup() // leftover from an earlier process that had our pid
	defer cleanup()

	if err := stageEntryInputs(staging, lockPath, hash); err != nil {
		return err
	}
	if err := ops.install(staging); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(staging, "node_modules")); err != nil {
		return fmt.Errorf("npm ci completed but node_modules missing in %s: %w", staging, err)
	}
	if ops.finalize != nil {
		if err := ops.finalize(staging); err != nil {
			return err
		}
	}

	if err := os.Rename(staging, entry); err != nil {
		if _, statErr := os.Stat(nodeModules); statErr == nil {
			logf("cache entry %s was completed by another process; using it", shortHash(hash))
			return nil
		}
		return fmt.Errorf("committing cache entry %s: %w", entry, err)
	}
	return nil
}
