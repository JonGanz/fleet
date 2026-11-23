package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
)

// staleLockAge is how old a lock file with no parseable pid may get before
// it's treated as abandoned. The owner writes its pid right after creating
// the file, so an empty/garbled lock older than this means the owner died in
// between. A variable so tests can shorten it.
var staleLockAge = time.Minute

// lockTicket takes the advisory lock for one ticket's state file (path is
// <tasks dir>/<ticket>.json) and holds it until the returned unlock is
// called. Mutating commands (new, edit, rm, mv) take it *before* reading the
// state and keep it for their whole run, so two concurrent invocations on
// the same ticket can't interleave read-modify-write cycles and lose an
// update. Different tickets never contend: each has its own lock.
//
// The lock is a "<path>.lock" sibling created with O_EXCL holding the
// owner's pid -- deliberately not flock(2), which would need an OS-specific
// dependency; per-ticket files already make cross-ticket contention
// impossible, so this only guards the rare same-ticket race.
//
// It fails fast instead of waiting: a command can legitimately hold the lock
// for many minutes (npm ci), so waiting would mostly just delay the same
// error. A lock is taken over only when it is stale (see lockIsStale).
func lockTicket(path string) (unlock func(), err error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(lockPath), err)
	}
	// Two attempts: the second follows a successful stale-lock takeover.
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, werr := fmt.Fprintf(f, "%d\n", os.Getpid())
			cerr := f.Close()
			if werr != nil || cerr != nil {
				os.Remove(lockPath)
				return nil, fmt.Errorf("write lock %s: %v", lockPath, errors.Join(werr, cerr))
			}
			return func() { releaseLock(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock %s: %w", lockPath, err)
		}

		contents, mtime, readErr := readLock(lockPath)
		if os.IsNotExist(readErr) {
			continue // released between our create and our read
		}
		if readErr != nil {
			return nil, fmt.Errorf("read lock %s: %w", lockPath, readErr)
		}
		if !lockIsStale(contents, mtime, time.Now(), staleLockAge, pidAlive) {
			return nil, lockHeldError(lockPath, contents)
		}
		if err := takeOverStaleLock(lockPath); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("could not take lock %s: another fleet-task took it first; retry", lockPath)
}

// lockHeldError explains who holds a live lock and how to clear it if that
// owner is actually gone (e.g. pid reuse).
func lockHeldError(lockPath, contents string) error {
	owner := "an unknown process"
	if pid, ok := parseLockPid(contents); ok {
		owner = fmt.Sprintf("pid %d", pid)
	}
	return fmt.Errorf("task is locked by another fleet-task (%s, lock file %s); wait for it to finish, or if no other fleet-task is running, remove the lock with `rm %s`", owner, lockPath, lockPath)
}

// lockIsStale decides whether a lock file can be taken over. A live owner
// pid never makes it stale, however old (commands hold the lock for their
// whole run, which can be many minutes); a dead owner always does. A lock
// with no parseable pid (the owner died between creating the file and
// writing its pid) is stale once older than maxAge. alive is injected so
// this stays a pure, testable function.
func lockIsStale(contents string, mtime, now time.Time, maxAge time.Duration, alive func(pid int) bool) bool {
	pid, ok := parseLockPid(contents)
	if !ok {
		return now.Sub(mtime) > maxAge
	}
	return !alive(pid)
}

// parseLockPid extracts the owner pid from a lock file's contents.
func parseLockPid(contents string) (int, bool) {
	pid, err := strconv.Atoi(strings.TrimSpace(contents))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// pidAlive reports whether a process with the given pid exists, via signal
// 0. EPERM means it exists but belongs to someone else.
func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func readLock(lockPath string) (contents string, mtime time.Time, err error) {
	info, err := os.Stat(lockPath)
	if err != nil {
		return "", time.Time{}, err
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return "", time.Time{}, err
	}
	return string(data), info.ModTime(), nil
}

// releaseLock removes lockPath only if it still holds our own pid, so an
// unlock can never delete a lock some other process legitimately owns.
func releaseLock(lockPath string) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	if pid, ok := parseLockPid(string(data)); ok && pid == os.Getpid() {
		os.Remove(lockPath)
	}
}

// takeOverStaleLock removes a lock judged stale without racing another
// process doing the same: a plain re-check-then-remove could delete a
// *fresh* lock that a faster process created in between. Instead the lock is
// renamed to a name unique to us (rename is atomic, so exactly one process
// gets any given lock file), and its contents are re-checked there. If what
// we grabbed turns out to be live after all, it is put back with a no-clobber
// link and we report it as held.
func takeOverStaleLock(lockPath string) error {
	grabbed := fmt.Sprintf("%s.stale-%d-%d", lockPath, os.Getpid(), time.Now().UnixNano())
	if err := os.Rename(lockPath, grabbed); err != nil {
		if os.IsNotExist(err) {
			return nil // someone else already cleared it; just retry the create
		}
		return fmt.Errorf("taking over stale lock %s: %w", lockPath, err)
	}
	defer os.Remove(grabbed)

	contents, mtime, err := readLock(grabbed)
	if err != nil {
		return fmt.Errorf("reading stale lock %s: %w", grabbed, err)
	}
	if lockIsStale(contents, mtime, time.Now(), staleLockAge, pidAlive) {
		cli.Warnf("removed stale lock %s (its owner is gone)", lockPath)
		return nil
	}
	// We raced another process and took its fresh lock: restore it. Link
	// (not rename) so we never overwrite a lock created meanwhile.
	if err := os.Link(grabbed, lockPath); err != nil {
		return fmt.Errorf("lock %s changed hands while taking it over (%v); retry", lockPath, err)
	}
	return lockHeldError(lockPath, contents)
}
