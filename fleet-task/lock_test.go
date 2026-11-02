package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseLockPid(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"1234\n", 1234, true},
		{"  7 ", 7, true},
		{"", 0, false},
		{"abc", 0, false},
		{"0", 0, false},
		{"-5", 0, false},
	}
	for _, c := range cases {
		got, ok := parseLockPid(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("parseLockPid(%q) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestLockIsStale(t *testing.T) {
	now := time.Now()
	alive := func(int) bool { return true }
	dead := func(int) bool { return false }
	maxAge := 5 * time.Minute

	if lockIsStale("100\n", now, now, maxAge, alive) {
		t.Error("fresh lock with live owner should not be stale")
	}
	if !lockIsStale("100\n", now, now, maxAge, dead) {
		t.Error("fresh lock with dead owner should be stale")
	}
	if !lockIsStale("100\n", now.Add(-10*time.Minute), now, maxAge, alive) {
		t.Error("old lock should be stale even if pid is alive (pid reuse)")
	}
	if lockIsStale("", now, now, maxAge, dead) {
		t.Error("fresh lock with no pid yet should not be stale (owner may be mid-create)")
	}
	if !lockIsStale("", now.Add(-10*time.Minute), now, maxAge, alive) {
		t.Error("old lock with no pid should be stale")
	}
}

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("own pid should be alive")
	}
	if pidAlive(0x7ffffff0) {
		t.Error("absurd pid should not be alive")
	}
}

func TestLockFileCreateAndRelease(t *testing.T) {
	target := filepath.Join(t.TempDir(), "T-1.json")
	unlock, err := lockFile(target)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if pid, ok := parseLockPid(string(data)); !ok || pid != os.Getpid() {
		t.Errorf("lock contents %q, want own pid", data)
	}
	unlock()
	if _, err := os.Stat(target + ".lock"); !os.IsNotExist(err) {
		t.Errorf("lock not removed on unlock: %v", err)
	}
}

func TestLockFileTakesOverDeadOwner(t *testing.T) {
	target := filepath.Join(t.TempDir(), "T-1.json")
	if err := os.WriteFile(target+".lock", []byte("2147483000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockFile(target)
	if err != nil {
		t.Fatalf("expected takeover of stale lock, got %v", err)
	}
	unlock()
}

func TestLockFileLiveOwnerTimesOutWithHelpfulError(t *testing.T) {
	old := lockTimeout
	lockTimeout = 150 * time.Millisecond
	defer func() { lockTimeout = old }()

	target := filepath.Join(t.TempDir(), "T-1.json")
	lockPath := target + ".lock"
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := lockFile(target)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), lockPath) || !strings.Contains(err.Error(), "rm ") {
		t.Errorf("error should name the lock file and how to clear it: %v", err)
	}
}
