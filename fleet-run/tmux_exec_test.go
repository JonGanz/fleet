package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWindowsWorktreeWinPathResolvesSymlink guards the fix in tmux_exec.go's
// newWindow: fleet-task creates the WSL-side worktree location as a symlink
// for runtime: windows repos, and wslpath -w must see through it (translating
// the *target*, not the symlink's own WSL-side path) or run commands would
// cd into the wrong (UNC) location on the Windows side.
func TestWindowsWorktreeWinPathResolvesSymlink(t *testing.T) {
	if _, err := os.Stat("/usr/bin/wslpath"); err != nil {
		t.Skip("wslpath not available in this environment")
	}

	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	wantWin, err := wslToWindowsPath(real)
	if err != nil {
		t.Fatalf("wslToWindowsPath(real): %v", err)
	}

	gotWin, err := windowsWorktreeWinPath(link)
	if err != nil {
		t.Fatalf("windowsWorktreeWinPath(link): %v", err)
	}

	if gotWin != wantWin {
		t.Errorf("windowsWorktreeWinPath(link) = %q, want %q (same as resolving real dir directly)", gotWin, wantWin)
	}
}

// TestWindowsWorktreeWinPathNonSymlink confirms the fix is a no-op for a
// plain (non-symlinked) directory.
func TestWindowsWorktreeWinPathNonSymlink(t *testing.T) {
	if _, err := os.Stat("/usr/bin/wslpath"); err != nil {
		t.Skip("wslpath not available in this environment")
	}

	dir := t.TempDir()

	want, err := wslToWindowsPath(dir)
	if err != nil {
		t.Fatalf("wslToWindowsPath: %v", err)
	}
	got, err := windowsWorktreeWinPath(dir)
	if err != nil {
		t.Fatalf("windowsWorktreeWinPath: %v", err)
	}
	if got != want {
		t.Errorf("windowsWorktreeWinPath(plain dir) = %q, want %q", got, want)
	}
}
