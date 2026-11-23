package wsl

import (
	"strings"
	"testing"
)

func TestPSQuote(t *testing.T) {
	cases := map[string]string{"it's": "'it''s'", "a'b": "'a''b'", "": "''", `C:\x y`: `'C:\x y'`}
	for in, want := range cases {
		if got := PSQuote(in); got != want {
			t.Errorf("PSQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommandScript(t *testing.T) {
	s := CommandScript(`C:\a b`, "git", "worktree", "add", "x'y")
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

func TestCommandScriptAbortsOnCdFailure(t *testing.T) {
	s := CommandScript(`C:\it's`, "npm", "ci")
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

func TestDriveLetter(t *testing.T) {
	cases := []struct{ path, want string }{
		{`C:\fleet\cache`, "C:"},
		{`d:\fleet\cache`, "D:"},
		{`\\wsl.localhost\Ubuntu\home\jon`, ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := DriveLetter(c.path); got != c.want {
			t.Errorf("DriveLetter(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestCheckSameDrive(t *testing.T) {
	if err := CheckSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": `c:\fleet\worktrees`,
	}); err != nil {
		t.Errorf("same-drive: %v", err)
	}

	err := CheckSameDrive(map[string]string{
		"b": `D:\fleet\worktrees`,
		"a": `C:\fleet\base`,
	})
	if err == nil || !strings.HasPrefix(err.Error(), "a (C:) and b (D:)") {
		t.Errorf("cross-drive: want deterministic error, got %v", err)
	}

	if err := CheckSameDrive(map[string]string{
		"a": `C:\fleet\base`,
		"b": "",
	}); err != nil {
		t.Errorf("with blank entry: %v", err)
	}
}

func TestCheckSameDriveRejectsPathWithoutDriveLetter(t *testing.T) {
	// Run repeatedly: with map iteration, an order-dependent check would
	// pass or fail depending on which label it saw first.
	for i := 0; i < 50; i++ {
		err := CheckSameDrive(map[string]string{
			"windows_base":          `C:\fleet\base`,
			"windows_worktree_root": `\\wsl.localhost\Ubuntu\home\jon\wt`,
		})
		if err == nil {
			t.Fatal("UNC path without a drive letter: want error, got nil")
		}
		if !strings.Contains(err.Error(), "windows_worktree_root") || !strings.Contains(err.Error(), "no drive letter") {
			t.Fatalf("error should name the offending path clearly: %v", err)
		}
	}
}

func TestCheckSameDriveDeterministicMessage(t *testing.T) {
	labeled := map[string]string{"b": `D:\x`, "a": `C:\x`, "c": `C:\y`}
	first := CheckSameDrive(labeled)
	if first == nil {
		t.Fatal("want cross-drive error")
	}
	for i := 0; i < 50; i++ {
		if err := CheckSameDrive(labeled); err == nil || err.Error() != first.Error() {
			t.Fatalf("message changed between runs: %v vs %v", first, err)
		}
	}
	if !strings.HasPrefix(first.Error(), "a (C:) and b (D:)") {
		t.Errorf("labels should be compared in sorted order: %v", first)
	}
}
