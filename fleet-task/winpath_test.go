package main

import (
	"strings"
	"testing"
)

func TestPsQuote(t *testing.T) {
	if got, want := psQuote("it's"), "'it''s'"; got != want {
		t.Errorf("psQuote = %q, want %q", got, want)
	}
}

func TestWindowsCommandScript(t *testing.T) {
	s := windowsCommandScript(`C:\a b`, "git", "worktree", "add", "x'y")
	for _, want := range []string{
		"$ErrorActionPreference='Stop'",
		"Set-Location -LiteralPath 'C:\\a b'",
		"& 'git' 'worktree' 'add' 'x''y'",
		"exit $LASTEXITCODE",
		"catch",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
	// The cd must happen under Stop, before the preference is relaxed.
	if strings.Index(s, "Set-Location") > strings.Index(s, "'Continue'") {
		t.Errorf("Set-Location must run before relaxing ErrorActionPreference:\n%s", s)
	}
}

func TestWindowsMkdirScript(t *testing.T) {
	s := windowsMkdirScript(`C:\x`)
	if !strings.Contains(s, "$ErrorActionPreference='Stop'") || !strings.Contains(s, "catch") || !strings.Contains(s, "exit 1") {
		t.Errorf("mkdir script must fail non-zero on cmdlet errors:\n%s", s)
	}
	if strings.Contains(s, "$LASTEXITCODE") {
		t.Errorf("mkdir script must not rely on $LASTEXITCODE after a cmdlet:\n%s", s)
	}
}

func TestWindowsGitRealPath(t *testing.T) {
	if _, err := (windowsGit{cfg: &ReposConfig{}}).realPath("/wt/T-1/ui"); err == nil {
		t.Error("expected an error when windows_worktree_root is unset")
	}
	if _, err := (windowsGit{}).realPath("/wt/T-1/ui"); err == nil {
		t.Error("expected an error with nil config")
	}
	got, err := (windowsGit{cfg: &ReposConfig{WindowsWorktreeRoot: "/mnt/c/fleet-wt"}}).realPath("/home/u/wt/T-1/ui")
	if err != nil || got != "/mnt/c/fleet-wt/T-1/ui" {
		t.Errorf("realPath = %q, %v", got, err)
	}
}

func TestStderrSuffix(t *testing.T) {
	if got := stderrSuffix("  \n"); got != "" {
		t.Errorf("blank: %q", got)
	}
	if got := stderrSuffix("fatal: nope\n"); got != ": fatal: nope" {
		t.Errorf("got %q", got)
	}
}
