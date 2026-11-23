package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLockIsStale(t *testing.T) {
	now := time.Now()
	alive := func(int) bool { return true }
	dead := func(int) bool { return false }
	maxAge := time.Minute

	if lockIsStale("100\n", now, now, maxAge, alive) {
		t.Error("fresh lock with live owner should not be stale")
	}
	if lockIsStale("100\n", now.Add(-time.Hour), now, maxAge, alive) {
		t.Error("old lock with live owner must not be stale (long-running command)")
	}
	if !lockIsStale("100\n", now, now, maxAge, dead) {
		t.Error("fresh lock with dead owner should be stale")
	}
	if lockIsStale("", now, now, maxAge, dead) {
		t.Error("fresh lock with no pid yet should not be stale (owner may be mid-create)")
	}
	if !lockIsStale("garbage", now.Add(-10*time.Minute), now, maxAge, alive) {
		t.Error("old lock with no pid should be stale")
	}
}

func TestLockTicketCreateAndRelease(t *testing.T) {
	target := filepath.Join(t.TempDir(), "tasks", "T-1.json")
	unlock, err := lockTicket(target)
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

func TestLockTicketLiveOwnerFailsFast(t *testing.T) {
	target := filepath.Join(t.TempDir(), "T-1.json")
	lockPath := target + ".lock"
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := lockTicket(target)
	if err == nil {
		t.Fatal("expected an error for a live lock")
	}
	if time.Since(start) > time.Second {
		t.Error("should fail fast, not wait")
	}
	if !strings.Contains(err.Error(), lockPath) || !strings.Contains(err.Error(), "`fleet-task unlock T-1`") {
		t.Errorf("error should name the lock file and how to clear it: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("live lock must be left in place: %v", err)
	}
}

func TestLockTicketSecondHolderRefused(t *testing.T) {
	target := filepath.Join(t.TempDir(), "T-1.json")
	unlock, err := lockTicket(target)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := lockTicket(target); err == nil {
		t.Error("a held lock must not be granted twice")
	}
}

func TestLockTicketTakesOverDeadOwner(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "T-1.json")
	if err := os.WriteFile(target+".lock", []byte("2147483000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockTicket(target)
	if err != nil {
		t.Fatalf("expected takeover of stale lock, got %v", err)
	}
	unlock()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("takeover left files behind: %v", entries)
	}
}

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

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("own pid should be alive")
	}
	if pidAlive(0x7ffffff0) {
		t.Error("absurd pid should not be alive")
	}
}

func TestReleaseLockLeavesForeignLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "T-1.json.lock")
	if err := os.WriteFile(lockPath, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseLock(lockPath)
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("releaseLock removed a lock it does not own: %v", err)
	}
}

func TestTakeOverStaleLockRestoresLiveLock(t *testing.T) {
	// Simulates losing the race: by the time we grab the file, it holds a
	// live owner's pid. It must be put back, not deleted.
	lockPath := filepath.Join(t.TempDir(), "T-1.json.lock")
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := takeOverStaleLock(lockPath); err == nil {
		t.Error("expected a held-lock error")
	}
	data, err := os.ReadFile(lockPath)
	if err != nil || strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("live lock not restored: %q, %v", data, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(lockPath))
	if len(entries) != 1 {
		t.Errorf("expected only the restored lock, got %v", entries)
	}
}
