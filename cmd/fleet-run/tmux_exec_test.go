package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JonGanz/fleet/internal/wsl"
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

	wantWin, err := wsl.WindowsPath(real)
	if err != nil {
		t.Fatalf("wsl.WindowsPath(real): %v", err)
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

	want, err := wsl.WindowsPath(dir)
	if err != nil {
		t.Fatalf("wsl.WindowsPath: %v", err)
	}
	got, err := windowsWorktreeWinPath(dir)
	if err != nil {
		t.Fatalf("windowsWorktreeWinPath: %v", err)
	}
	if got != want {
		t.Errorf("windowsWorktreeWinPath(plain dir) = %q, want %q", got, want)
	}
}

func TestCheckWorktreeDir(t *testing.T) {
	dir := t.TempDir()
	if err := checkWorktreeDir(dir); err != nil {
		t.Errorf("existing dir: %v", err)
	}
	if err := checkWorktreeDir(dir + "/missing"); err == nil {
		t.Error("expected error for missing dir")
	}
	f := dir + "/file"
	if err := os.WriteFile(f, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkWorktreeDir(f); err == nil {
		t.Error("expected error for non-directory")
	}
}

func TestParseWindowLines(t *testing.T) {
	got, err := parseWindowLines("@1\tbackend-api\n@12\tmy.app-dev\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (tmuxWindow{"@1", "backend-api"}) || got[1] != (tmuxWindow{"@12", "my.app-dev"}) {
		t.Errorf("got %v", got)
	}
	if names := windowNames(got); len(names) != 2 || names[1] != "my.app-dev" {
		t.Errorf("windowNames = %v", names)
	}
	for _, bad := range []string{"noTab", "1\tname"} {
		if _, err := parseWindowLines(bad); err == nil {
			t.Errorf("parseWindowLines(%q) should fail", bad)
		}
	}
}
