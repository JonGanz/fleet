package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stagingInfix separates a cache entry's key from its staging suffix:
// <key>.tmp-<pid>. Entries are built in a staging dir and renamed into
// place, so an entry directory only ever exists once it is complete.
const stagingInfix = ".tmp-"

// entryCompleteMarker is written at the root of a staging dir once the
// entry is fully built (install and finalize both succeeded), just before
// the rename into place. An entry dir without it is never trusted: that
// catches legacy entries built before staging existed, which may hold a
// partial node_modules.
const entryCompleteMarker = ".fleet-cache-complete"

// npmrcName is the project-level npm config file. It is copied into the
// staging dir so `npm ci` honours it (private registry, legacy-peer-deps,
// ...), and removed again after the install because it may hold a
// registry token that must never persist in the shared cache.
const npmrcName = ".npmrc"

// stagingMaxAge is how old a staging dir must be before gc flags it as
// abandoned (a live `ensure` is never expected to take this long).
const stagingMaxAge = 24 * time.Hour

// entryOps are the platform-specific pieces of building a cache entry.
type entryOps struct {
	// install runs `npm ci` inside stagingDir (a WSL/Linux-form path),
	// stopping it when ctx is cancelled.
	install func(ctx context.Context, stagingDir string) error
	// finalize, if non-nil, runs after a successful install (e.g.
	// lockDownPermissions on the Linux path).
	finalize func(stagingDir string) error
	// remove deletes a directory tree (os.RemoveAll, or a native Windows
	// bulk delete).
	remove func(dir string) error
}

// checkNoWorkspaces errors if the package.json at pkgPath declares npm
// workspaces (a non-empty "workspaces" value). srcDir is only used in the
// messages, pointing the user at their worktree rather than the cache.
func checkNoWorkspaces(pkgPath, srcDir string) error {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return fmt.Errorf("reading package.json: %w", err)
	}
	declared, err := declaresWorkspaces(data)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Join(srcDir, "package.json"), err)
	}
	if declared {
		return fmt.Errorf("%s declares npm workspaces, which fleet-cache does not support (the workspace packages are not available to `npm ci` in the cache); run `npm ci` in %s directly instead", filepath.Join(srcDir, "package.json"), srcDir)
	}
	return nil
}

// copyFile copies src to dst, creating dst with perm (subject to umask) or
// truncating it if it already exists.
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// declaresWorkspaces reports whether package.json content data has a
// "workspaces" field that is neither null nor empty ([] or {}). Malformed
// JSON, or a top level that isn't an object, is an error.
func declaresWorkspaces(data []byte) (bool, error) {
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false, fmt.Errorf("not a valid package.json (expected a JSON object): %w", err)
	}
	raw, ok := manifest["workspaces"]
	if !ok {
		return false, nil
	}
	switch string(bytes.Join(bytes.Fields(raw), nil)) {
	case "null", "[]", "{}":
		return false, nil
	}
	return true, nil
}

// ensureCacheEntry makes sure the cache entry at entry (<node-cache>/<key>)
// exists and is complete. The entry is built in a unique staging dir
// (<entry>.tmp-<pid>) and renamed into place atomically, so:
//   - an entry directory only exists once fully built (a killed `npm ci`
//     leaves only a staging dir, never a reusable partial entry);
//   - concurrent ensures of the same key can't corrupt each other: the
//     loser's rename fails because the winner's entry exists, and it
//     discards its own staging dir and uses the winner's;
//   - the staging dir is removed on any failure, including ctx being
//     cancelled (an interrupt), which also stops the install.
//
// A project .npmrc is only present in the staging dir while install runs.
func ensureCacheEntry(ctx context.Context, entry, key, lockPath string, ops entryOps) error {
	complete, err := isEntryComplete(entry)
	if err != nil {
		return err
	}
	if complete {
		return nil
	}

	// An entry dir without the completion marker can only be a legacy entry
	// from before staging existed (possibly with a partial node_modules); it
	// can't be trusted, and rename can't replace it.
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

	if err := stageEntryInputs(staging, lockPath, key); err != nil {
		return err
	}
	installErr := ops.install(ctx, staging)
	npmrcErr := removeStagedNpmrc(staging)
	if err := interrupted(ctx); err != nil {
		return err
	}
	if installErr != nil {
		return installErr
	}
	if npmrcErr != nil {
		return npmrcErr
	}
	if _, err := os.Stat(filepath.Join(staging, "node_modules")); err != nil {
		return fmt.Errorf("npm ci completed but node_modules missing in %s: %w", staging, err)
	}
	if ops.finalize != nil {
		if err := ops.finalize(staging); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(staging, entryCompleteMarker), []byte(key+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing cache entry completion marker: %w", err)
	}
	// Last chance to honour an interrupt that arrived during finalize.
	if err := interrupted(ctx); err != nil {
		return err
	}

	if err := os.Rename(staging, entry); err != nil {
		if done, _ := isEntryComplete(entry); done {
			logf("cache entry %s was completed by another process; using it", shortHash(key))
			return nil
		}
		return fmt.Errorf("committing cache entry %s: %w", entry, err)
	}
	return nil
}

// interrupted returns a descriptive error if ctx was cancelled (by
// SIGINT/SIGTERM via interruptContext), else nil.
func interrupted(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	logf("interrupted; removing the cache staging dir")
	return errors.New("interrupted; cache entry not built")
}

// isEntryComplete reports whether entry carries entryCompleteMarker.
func isEntryComplete(entry string) (bool, error) {
	_, err := os.Stat(filepath.Join(entry, entryCompleteMarker))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking cache entry %s: %w", entry, err)
}

func isStagingName(name string) bool {
	return strings.Contains(name, stagingInfix)
}

// logf writes fleet-cache's own progress/status output to stderr, keeping
// stdout free for machine-readable output and for callers (fleet-task)
// that interleave several tools' output.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// removeStagedNpmrc deletes the .npmrc stageNpmrc may have copied into
// stagingDir, so a registry token never ends up in a committed entry.
func removeStagedNpmrc(stagingDir string) error {
	err := os.Remove(filepath.Join(stagingDir, npmrcName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing .npmrc from cache staging dir: %w", err)
	}
	return nil
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// stageEntryInputs creates stagingDir and copies package-lock.json,
// package.json and, if present, .npmrc (taken from lockPath's directory)
// into it.
//
// `npm ci` requires both package files in its working directory (it
// validates the lockfile against the manifest) -- copying the lockfile alone
// fails with an ENOENT reading package.json. The staged inputs are re-keyed
// (cacheKey on the staging dir) to make sure they still match wantKey (the
// lockfile or .npmrc could have changed since they were keyed), so an entry
// is never filed under the wrong key.
// A package.json declaring workspaces is rejected up front: the workspace
// packages aren't in the staging dir, so `npm ci` there can't work.
func stageEntryInputs(stagingDir, lockPath, wantKey string) error {
	srcDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("creating cache staging dir %s: %w", stagingDir, err)
	}
	stagedLock := filepath.Join(stagingDir, lockfileName)
	if err := copyFile(lockPath, stagedLock, 0o644); err != nil {
		return fmt.Errorf("copying package-lock.json into cache: %w", err)
	}
	stagedPkg := filepath.Join(stagingDir, "package.json")
	if err := copyFile(filepath.Join(srcDir, "package.json"), stagedPkg, 0o644); err != nil {
		return fmt.Errorf("copying package.json into cache: %w", err)
	}
	if err := checkNoWorkspaces(stagedPkg, srcDir); err != nil {
		return err
	}
	if err := stageNpmrc(srcDir, stagingDir); err != nil {
		return err
	}
	if _, got, err := cacheKey(stagingDir); err != nil || got != wantKey {
		return fmt.Errorf("package-lock.json or .npmrc changed while staging the cache entry; rerun")
	}
	return nil
}

// stageNpmrc copies <srcDir>/.npmrc into stagingDir with mode 0600 (it may
// hold a token) if it exists. Anything other than a regular file (or a
// symlink to one) under that name is an error rather than silently ignored,
// since npm would otherwise run without the config the project expects.
func stageNpmrc(srcDir, stagingDir string) error {
	src := filepath.Join(srcDir, npmrcName)
	info, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	if err := copyFile(src, filepath.Join(stagingDir, npmrcName), 0o600); err != nil {
		return fmt.Errorf("copying .npmrc into cache: %w", err)
	}
	return nil
}

// stagingPath returns this process's staging dir for entry.
func stagingPath(entry string) string {
	return fmt.Sprintf("%s%s%d", entry, stagingInfix, os.Getpid())
}
