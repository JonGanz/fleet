package main

import "testing"

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

func TestResolveWindowsCacheRootWithFlag(t *testing.T) {
	got, err := resolveWindowsCacheRoot("/mnt/c/fleet/cache-windows")
	if err != nil {
		t.Fatalf("resolveWindowsCacheRoot: %v", err)
	}
	if got != "/mnt/c/fleet/cache-windows" {
		t.Errorf("resolveWindowsCacheRoot(flag) = %q, want flag value passed through unchanged", got)
	}
}
