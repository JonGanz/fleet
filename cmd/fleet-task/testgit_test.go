package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireGit skips the test without a git binary and isolates git from the
// user's config, giving commits a fixed identity.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("FLEET_NO_INPUT", "1")
}

// gitT runs git in dir and returns trimmed stdout, failing the test on error.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	outb, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, outb)
	}
	return strings.TrimSpace(string(outb))
}

// makeOrigin creates a normal repo with one commit on main (file README)
// to act as a remote, and returns its path.
func makeOrigin(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "origin")
	gitT(t, filepath.Dir(dir), "init", "-q", "-b", "main", dir)
	writeFileT(t, filepath.Join(dir, "README"), "hello\n")
	gitT(t, dir, "add", "README")
	gitT(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// writeFileT writes content to path (creating parent dirs), failing the
// test on error.
func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
