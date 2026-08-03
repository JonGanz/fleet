package main

import (
	"fmt"
	"os"
	"time"
)

// lockFile implements a simple, portable advisory lock for a given target
// path by creating a "<path>.lock" sibling file with O_EXCL and spinning
// briefly if it's already held. This is deliberately not a real flock(2)
// syscall (which would pull in an OS-specific dependency such as
// golang.org/x/sys/unix) -- since each ticket has its own state file and
// contention is only ever between two invocations touching the *same*
// ticket at nearly the same instant, a simple exclusive-create lockfile
// with a short retry/timeout is sufficient as the "safety belt" the
// contract calls for. It returns an unlock function that must be called to
// release the lock (removes the lockfile).
func lockFile(path string) (unlock func(), err error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for lock %s", lockPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
