package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
)

// unlockResult is `fleet-task unlock --json`'s document.
type unlockResult struct {
	Ticket   string `json:"ticket"`
	LockFile string `json:"lock_file"`
	// HadLock: a lock file was there.
	HadLock bool `json:"had_lock"`
	Removed bool `json:"removed"`
	// OwnerPid is the pid recorded in the lock, if readable.
	OwnerPid *int `json:"owner_pid,omitempty"`
	// OwnerAlive: a process with OwnerPid is running.
	OwnerAlive bool   `json:"owner_alive"`
	Error      string `json:"error,omitempty"`
}

// unlockDecision is decideUnlock's verdict on one lock file.
type unlockDecision struct {
	Remove bool
	// Refusal explains why the lock stays (Remove false).
	Refusal string
}

// cmdUnlock removes ticket's lock file left behind by a fleet-task that is
// gone (killed, crashed): only when the recorded owner pid is not running,
// or the lock has no readable pid and is older than staleLockAge (the rule
// lockTicket itself uses). A live owner is refused (exit 1) unless force,
// which is for a pid that was reused by some unrelated process. No lock is
// success. A lock file without a state file (an interrupted `new`) may
// still be removed; a ticket with neither is "no task found" (exit 2).
func cmdUnlock(ticket string, force, jsonOut bool) error {
	useJSONOutput(jsonOut)
	if ticket == "" {
		return cli.Usagef("unlock needs a ticket: `fleet-task unlock <ticket>`")
	}
	tf, err := taskFile(ticket) // validates the id
	if err != nil {
		return err
	}
	lockPath := tf + ".lock"
	res := unlockResult{Ticket: ticket, LockFile: lockPath}

	contents, mtime, err := readLock(lockPath)
	switch {
	case os.IsNotExist(err):
		if terr := requireTaskExists(ticket, tf); terr != nil {
			return terr
		}
		fmt.Fprintf(out, "task %s is not locked\n", ticket)
		return printUnlock(res, jsonOut, nil)
	case err != nil:
		return printUnlock(res, jsonOut, fmt.Errorf("read lock %s: %w", lockPath, err))
	}
	res.HadLock = true
	pid, havePid := parseLockPid(contents)
	if havePid {
		res.OwnerPid = &pid
		res.OwnerAlive = pidAlive(pid)
	}

	d := decideUnlock(havePid, res.OwnerAlive, time.Since(mtime), staleLockAge, force)
	if !d.Remove {
		return printUnlock(res, jsonOut, fmt.Errorf("lock %s not removed: %s", lockPath, d.Refusal))
	}
	if err := removeLockFile(lockPath, contents); err != nil {
		return printUnlock(res, jsonOut, err)
	}
	res.Removed = true
	if force && res.OwnerAlive {
		cli.Warnf("removed lock %s although pid %d is running (--force)", lockPath, pid)
	}
	fmt.Fprintf(out, "removed lock %s\n", lockPath)
	return printUnlock(res, jsonOut, nil)
}

// decideUnlock is cmdUnlock's pure decision. A live owner is never
// overridden without force; a lock with no readable pid is only removed
// once older than maxAge, since its owner may be about to write its pid.
func decideUnlock(havePid, ownerAlive bool, age, maxAge time.Duration, force bool) unlockDecision {
	switch {
	case force:
		return unlockDecision{Remove: true}
	case havePid && ownerAlive:
		return unlockDecision{Refusal: "its owner (the recorded pid) is still running -- a fleet-task may still be working on this ticket; wait for it to finish, or, only if that pid is not a fleet-task, pass --force"}
	case havePid:
		return unlockDecision{Remove: true}
	case age > maxAge:
		return unlockDecision{Remove: true}
	default:
		return unlockDecision{Refusal: fmt.Sprintf("it has no readable owner pid and is less than %s old, so its owner may still be starting; retry shortly, or pass --force", maxAge)}
	}
}

// printUnlock prints res (with jsonOut; Error filled from err) and returns
// err.
func printUnlock(res unlockResult, jsonOut bool, err error) error {
	if err != nil {
		res.Error = err.Error()
	}
	if jsonOut {
		if perr := cli.PrintJSON(res); perr != nil && err == nil {
			return perr
		}
	}
	return err
}

// removeLockFile removes lockPath only if it still holds contents (what the
// decision was based on): the lock is first renamed aside, which is atomic,
// so a lock some other fleet-task created in the meantime is never deleted
// -- if what was grabbed differs, it is put back with a no-clobber link.
func removeLockFile(lockPath, contents string) error {
	grabbed := fmt.Sprintf("%s.unlock-%d-%d", lockPath, os.Getpid(), time.Now().UnixNano())
	if err := os.Rename(lockPath, grabbed); err != nil {
		if os.IsNotExist(err) {
			return nil // already gone
		}
		return fmt.Errorf("removing lock %s: %w", lockPath, err)
	}
	data, err := os.ReadFile(grabbed)
	if err == nil && string(data) != contents {
		lerr := os.Link(grabbed, lockPath)
		os.Remove(grabbed)
		if lerr != nil {
			return fmt.Errorf("lock %s changed while removing it and could not be put back (%v); check it", lockPath, lerr)
		}
		return errors.New("lock " + lockPath + " was taken by another fleet-task meanwhile; left in place")
	}
	if err := os.Remove(grabbed); err != nil {
		return fmt.Errorf("removing lock %s: %w", grabbed, err)
	}
	return nil
}
