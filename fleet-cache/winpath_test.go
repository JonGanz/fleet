package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWinJoin(t *testing.T) {
	cases := []struct{ base, sub, want string }{
		{`C:\fleet\cache`, "node-cache", `C:\fleet\cache\node-cache`},
		{`C:\fleet\cache\`, `\node-cache`, `C:\fleet\cache\node-cache`},
		{`C:\Users\jon\AppData\Local`, "fleet", `C:\Users\jon\AppData\Local\fleet`},
	}
	for _, c := range cases {
		if got := winJoin(c.base, c.sub); got != c.want {
			t.Errorf("winJoin(%q, %q) = %q, want %q", c.base, c.sub, got, c.want)
		}
	}
}

func TestDriveLetter(t *testing.T) {
	cases := []struct{ path, want string }{
		{`C:\fleet\cache`, "C:"},
		{`d:\fleet\cache`, "D:"},
		{`\\wsl.localhost\Ubuntu\home\jon`, ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := driveLetter(c.path); got != c.want {
			t.Errorf("driveLetter(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestCheckSameDrive(t *testing.T) {
	if err := checkSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": `C:\fleet\worktrees`,
	}); err != nil {
		t.Errorf("same-drive: %v", err)
	}

	if err := checkSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": `D:\fleet\worktrees`,
	}); err == nil {
		t.Error("cross-drive: want error, got nil")
	}

	if err := checkSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": "",
	}); err != nil {
		t.Errorf("with blank entry: %v", err)
	}
}

func TestCheckSameDriveRejectsPathWithoutDriveLetter(t *testing.T) {
	// Run repeatedly: with map iteration, the old code passed or failed
	// depending on which label it saw first.
	for i := 0; i < 50; i++ {
		err := checkSameDrive(map[string]string{
			"cache entry": `C:\fleet\cache`,
			"worktree":    `\\wsl.localhost\Ubuntu\home\jon\wt`,
		})
		if err == nil {
			t.Fatal("UNC path without a drive letter: want error, got nil")
		}
		if !strings.Contains(err.Error(), "worktree") || !strings.Contains(err.Error(), "no drive letter") {
			t.Fatalf("error should name the offending path clearly: %v", err)
		}
	}
}

func TestCheckSameDriveDeterministicMessage(t *testing.T) {
	labeled := map[string]string{"b": `D:\x`, "a": `C:\x`, "c": `C:\y`}
	first := checkSameDrive(labeled)
	if first == nil {
		t.Fatal("want cross-drive error")
	}
	for i := 0; i < 50; i++ {
		if err := checkSameDrive(labeled); err == nil || err.Error() != first.Error() {
			t.Fatalf("message changed between runs: %v vs %v", first, err)
		}
	}
	if !strings.HasPrefix(first.Error(), "a (C:) and b (D:)") {
		t.Errorf("labels should be compared in sorted order: %v", first)
	}
}

func TestResolveWindowsCacheRootWithFlag(t *testing.T) {
	got, err := resolveWindowsCacheRoot("/mnt/c/fleet/cache-windows")
	if err != nil {
		t.Fatalf("resolveWindowsCacheRoot: %v", err)
	}
	if got != "/mnt/c/fleet/cache-windows" {
		t.Errorf("resolveWindowsCacheRoot(flag) = %q, want flag value passed through unchanged", got)
	}
}

func TestWindowsCommandScriptAbortsOnCdFailure(t *testing.T) {
	s := windowsCommandScript(`C:\it's`, []string{"npm", "ci"})
	for _, want := range []string{
		"$ErrorActionPreference='Stop'",
		`Set-Location -LiteralPath 'C:\it''s'`,
		"& 'npm' 'ci'",
		"exit $LASTEXITCODE",
		"catch",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "Set-Location") > strings.Index(s, "& 'npm'") {
		t.Error("cd must come before the command inside the try block")
	}
}

func TestWindowsRemoveScriptFailsOnCmdletError(t *testing.T) {
	s := windowsRemoveScript(`C:\x`)
	for _, want := range []string{"$ErrorActionPreference='Stop'", "try {", "Remove-Item -LiteralPath 'C:\\x' -Recurse -Force", "exit 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
}

func TestParseLocalAppData(t *testing.T) {
	got, err := parseLocalAppData([]byte("\ufeffC:\\Users\\Jörg\\AppData\\Local\r\n"))
	if err != nil || got != `C:\Users\Jörg\AppData\Local` {
		t.Errorf("parseLocalAppData = %q, %v", got, err)
	}
	if _, err := parseLocalAppData([]byte("  \n")); err == nil {
		t.Error("empty output should error")
	}
}

func TestWriteHardlinkScriptOnlyWhenChanged(t *testing.T) {
	if _, err := exec.LookPath("wslpath"); err != nil {
		t.Skip("wslpath not available")
	}
	root := t.TempDir()
	if _, err := writeHardlinkScript(root); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "hardlink.ps1")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := writeHardlinkScript(root); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if !fi.ModTime().Before(time.Now().Add(-30 * time.Minute)) {
		t.Error("identical content should not be rewritten")
	}
	if err := os.WriteFile(p, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeHardlinkScript(root); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, hardlinkWindowsScript) {
		t.Error("differing content should be rewritten")
	}
}
