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

func TestParseLocalAppData(t *testing.T) {
	got, err := parseLocalAppData([]byte("\ufeffC:\\Users\\Jörg\\AppData\\Local\r\n"))
	if err != nil || got != `C:\Users\Jörg\AppData\Local` {
		t.Errorf("parseLocalAppData = %q, %v", got, err)
	}
	if _, err := parseLocalAppData([]byte("  \n")); err == nil {
		t.Error("empty output should error")
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

func TestWindowsRemoveScriptFailsOnCmdletError(t *testing.T) {
	s := windowsRemoveScript(`C:\x`)
	for _, want := range []string{"$ErrorActionPreference='Stop'", "try {", "Remove-Item -LiteralPath 'C:\\x' -Recurse -Force", "exit 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
}

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
