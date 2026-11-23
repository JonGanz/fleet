//go:build unix

package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRunTimeoutKillsGroup: a child that started a background grandchild is
// killed by the timeout, grandchild included.
func TestRunTimeoutKillsGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command("/bin/sh", "-c", "sleep 60 & echo $! > "+pidFile+"; wait")
	start := time.Now()
	err := Run(cmd, Options{Timeout: 300 * time.Millisecond, NewGroup: true})
	if !IsTimeout(err) {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if !strings.Contains(err.Error(), "killed after the 300ms timeout") {
		t.Errorf("message = %q", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("Run took %s", time.Since(start))
	}
	data, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	// The grandchild was SIGKILLed with the group; give the kernel a moment
	// to reap it (its parent, the shell, is gone, so init reaps it).
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive after the group kill", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunNoTimeout(t *testing.T) {
	if err := Run(exec.Command("/bin/sh", "-c", "exit 0"), Options{Timeout: time.Minute, NewGroup: true}); err != nil {
		t.Errorf("clean exit: %v", err)
	}
	err := Run(exec.Command("/bin/sh", "-c", "exit 3"), Options{})
	if err == nil || IsTimeout(err) || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("failing exit: %v", err)
	}
	if err := Run(exec.Command(filepath.Join(t.TempDir(), "missing")), Options{NewGroup: true}); err == nil {
		t.Error("missing binary: want a start error")
	}
}
