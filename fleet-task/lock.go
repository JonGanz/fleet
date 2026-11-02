package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Lock tuning. Variables (not constants) so tests can shorten them.
var (
	// lockTimeout is how long lockFile waits on a lock that looks live.
	lockTimeout = 5 * time.Second
	// staleLockAge is how old a lock file may get before it's treated as
	// abandoned regardless of its pid (guards against pid reuse, and
	// against a lock whose pid was never written). A state write takes
	// milliseconds, so minutes is very conservative.
	staleLockAge = 5 * time.Minute
)

// lockFile implements a simple, portable advisory lock for a given target
// path by creating a "<path>.lock" sibling file with O_EXCL and spinning
// briefly if it's already held. This is deliberately not a real flock(2)
// syscall (which would pull in an OS-specific dependency such as
// golang.org/x/sys/unix) -- since each ticket has its own state file and
// contention is only ever between two invocations touching the *same*
// ticket at nearly the same instant, a simple exclusive-create lockfile
// with a short retry/timeout is sufficient as the "safety belt" the
// contract calls for.
//
// The lock file holds the owner's pid. A lock left behind by a crashed
// process (Ctrl-C between create and unlock) is detected -- owner pid no
// longer alive, or the file older than staleLockAge -- and taken over
// instead of blocking every later write to that ticket forever. If a lock
// that looks live is never released, the timeout error names the file and
// says how to clear it.
//
// It returns an unlock function that must be called to release the lock
// (removes the lockfile).
func lockFile(path string) (unlock func(), err error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock %s: %w", lockPath, err)
		}

		contents, mtime, statErr := readLock(lockPath)
		if statErr == nil && lockIsStale(contents, mtime, time.Now(), staleLockAge, pidAlive) {
			fmt.Fprintf(os.Stderr, "warning: removing stale lock %s (owner is gone)\n", lockPath)
			// Best effort. If another process takes over at the same
			// instant, the O_EXCL create above arbitrates on the next turn.
			os.Remove(lockPath)
			continue
		}

		if time.Now().After(deadline) {
			owner := ""
			if pid, ok := parseLockPid(contents); ok {
				owner = fmt.Sprintf(" (held by pid %d)", pid)
			}
			return nil, fmt.Errorf("timed out waiting for lock %s%s; if no other fleet-task is running, remove it with `rm %s`", lockPath, owner, lockPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
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

// parseLockPid extracts the owner pid from a lock file's contents.
func parseLockPid(contents string) (int, bool) {
	pid, err := strconv.Atoi(strings.TrimSpace(contents))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// lockIsStale decides whether a lock file can be taken over: its owner pid
// is no longer alive, or it's older than maxAge. A lock with no parseable
// pid (e.g. the owner died between creating the file and writing its pid)
// is only stale by age. alive is injected so this stays a pure, testable
// function.
func lockIsStale(contents string, mtime, now time.Time, maxAge time.Duration, alive func(pid int) bool) bool {
	if now.Sub(mtime) > maxAge {
		return true
	}
	pid, ok := parseLockPid(contents)
	if !ok {
		return false
	}
	return !alive(pid)
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
