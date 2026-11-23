package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/taskstate"
)

func TestDecideUnlock(t *testing.T) {
	const maxAge = time.Minute
	cases := []struct {
		name                  string
		havePid, alive, force bool
		age                   time.Duration
		wantRemove            bool
	}{
		{name: "dead owner", havePid: true, wantRemove: true},
		{name: "live owner", havePid: true, alive: true},
		{name: "live owner forced", havePid: true, alive: true, force: true, wantRemove: true},
		{name: "no pid, young", age: time.Second},
		{name: "no pid, old", age: 2 * time.Minute, wantRemove: true},
		{name: "no pid, young, forced", age: time.Second, force: true, wantRemove: true},
	}
	for _, c := range cases {
		d := decideUnlock(c.havePid, c.alive, c.age, maxAge, c.force)
		if d.Remove != c.wantRemove || (!d.Remove && d.Refusal == "") {
			t.Errorf("%s: got %+v", c.name, d)
		}
	}
}

// deadPid returns the pid of a process that has exited.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot run /bin/sh: %v", err)
	}
	return cmd.Process.Pid
}

func TestCmdUnlock(t *testing.T) {
	fleetEnv(t, "repos: []\n")
	tf, err := taskFile("T-1")
	if err != nil {
		t.Fatal(err)
	}
	lockPath := tf + ".lock"
	writeLock := func(pid int) {
		t.Helper()
		writeFileT(t, lockPath, strconv.Itoa(pid)+"\n")
	}

	// Unknown ticket, no lock: usage error. Invalid id: usage error.
	if err := cmdUnlock("T-1", false, false); !cli.IsUsage(err) || !strings.Contains(err.Error(), "no task found") {
		t.Errorf("unknown ticket: %v", err)
	}
	if err := cmdUnlock("../x", false, false); !cli.IsUsage(err) {
		t.Errorf("invalid ticket id: %v", err)
	}

	// A lock without a state file (interrupted new) with a dead owner goes.
	writeLock(deadPid(t))
	if err := cmdUnlock("T-1", false, false); err != nil {
		t.Fatalf("dead owner: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock still there: %v", err)
	}

	// With a state file and no lock: success.
	if err := taskstate.Write(tf, &taskstate.State{Ticket: "T-1", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := cmdUnlock("T-1", false, false); err != nil {
		t.Errorf("no lock: %v", err)
	}

	// A live owner (this test process) is refused, naming the pid...
	writeLock(os.Getpid())
	err = cmdUnlock("T-1", false, false)
	if err == nil || cli.IsUsage(err) || !strings.Contains(err.Error(), "still running") {
		t.Errorf("live owner: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("live lock removed: %v", err)
	}
	// ...unless forced.
	if err := cmdUnlock("T-1", true, false); err != nil {
		t.Errorf("forced: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("forced lock still there: %v", err)
	}

	// A fresh pid-less lock stays; an old one goes.
	writeFileT(t, lockPath, "")
	if err := cmdUnlock("T-1", false, false); err == nil {
		t.Error("fresh pid-less lock: want refusal")
	}
	old := time.Now().Add(-2 * staleLockAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := cmdUnlock("T-1", false, false); err != nil {
		t.Errorf("old pid-less lock: %v", err)
	}
	if matches, _ := filepath.Glob(lockPath + "*"); len(matches) != 0 {
		t.Errorf("leftovers: %v", matches)
	}
}

func TestRemoveLockFileKeepsChangedLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "T-1.json.lock")
	writeFileT(t, lockPath, "123\n")
	if err := removeLockFile(lockPath, "999\n"); err == nil {
		t.Error("a lock whose contents changed must not be removed")
	}
	if data, err := os.ReadFile(lockPath); err != nil || string(data) != "123\n" {
		t.Errorf("lock not put back: %q, %v", data, err)
	}
	if err := removeLockFile(lockPath, "123\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock still there: %v", err)
	}
}
