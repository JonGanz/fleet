package xdg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
)

func TestCacheDirPrecedence(t *testing.T) {
	t.Setenv("FLEET_CACHE_DIR", "/env/fleet")
	t.Setenv("XDG_CACHE_HOME", "/xdg")

	if got, _ := CacheDir("/flag"); got != "/flag" {
		t.Errorf("flag should win, got %q", got)
	}
	if got, _ := CacheDir(""); got != "/env/fleet" {
		t.Errorf("FLEET_CACHE_DIR should win over XDG, got %q", got)
	}
	t.Setenv("FLEET_CACHE_DIR", "")
	if got, _ := CacheDir(""); got != filepath.Join("/xdg", "fleet") {
		t.Errorf("XDG fallback, got %q", got)
	}
}

func TestConfigAndStatePrecedence(t *testing.T) {
	t.Setenv("FLEET_CONFIG_DIR", "/cfg")
	t.Setenv("FLEET_STATE_DIR", "/state")
	t.Setenv("FLEET_REPOS_FILE", "")
	if got, _ := ConfigDir(); got != "/cfg" {
		t.Errorf("ConfigDir = %q", got)
	}
	if got, _ := ReposFile(); got != "/cfg/repos.yaml" {
		t.Errorf("ReposFile = %q", got)
	}
	if got, _ := TasksDir(); got != "/state/tasks" {
		t.Errorf("TasksDir = %q", got)
	}
	if got, _ := DefaultWorktreeRoot(); got != "/state/worktrees" {
		t.Errorf("DefaultWorktreeRoot = %q", got)
	}
	if got, _ := PatchesDir("api"); got != "/cfg/patches/api" {
		t.Errorf("PatchesDir = %q", got)
	}
	if got, _ := HooksDir("pre-run"); got != "/cfg/hooks/pre-run" {
		t.Errorf("HooksDir = %q", got)
	}
	t.Setenv("FLEET_REPOS_FILE", "/elsewhere/r.yaml")
	if got, _ := ReposFile(); got != "/elsewhere/r.yaml" {
		t.Errorf("ReposFile override = %q", got)
	}

	t.Setenv("FLEET_CONFIG_DIR", "")
	t.Setenv("FLEET_STATE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xc")
	t.Setenv("XDG_STATE_HOME", "/xs")
	if got, _ := ConfigDir(); got != "/xc/fleet" {
		t.Errorf("ConfigDir XDG = %q", got)
	}
	if got, _ := StateDir(); got != "/xs/fleet" {
		t.Errorf("StateDir XDG = %q", got)
	}
}

func TestRelativeOverridesBecomeAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLEET_STATE_DIR", "rel/state")
	t.Setenv("FLEET_REPOS_FILE", "rel/repos.yaml")
	t.Setenv("FLEET_CACHE_DIR", "rel/cache")
	if got, _ := StateDir(); got != filepath.Join(wd, "rel/state") {
		t.Errorf("StateDir = %q", got)
	}
	if got, _ := ReposFile(); got != filepath.Join(wd, "rel/repos.yaml") {
		t.Errorf("ReposFile = %q", got)
	}
	if got, _ := CacheDir(""); got != filepath.Join(wd, "rel/cache") {
		t.Errorf("CacheDir = %q", got)
	}
	if got, _ := CacheDir("flagrel"); got != filepath.Join(wd, "flagrel") {
		t.Errorf("CacheDir(flag) = %q", got)
	}
}

func TestRelativeXDGIgnoredWithOneWarning(t *testing.T) {
	var buf bytes.Buffer
	defer cli.SetStderr(&buf)()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FLEET_CONFIG_DIR", "")
	t.Setenv("FLEET_STATE_DIR", "")
	t.Setenv("FLEET_CACHE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "relative-config-for-test")
	t.Setenv("XDG_STATE_HOME", "relative-state-for-test")
	t.Setenv("XDG_CACHE_HOME", "relative-cache-for-test")

	for i := 0; i < 3; i++ {
		if got, _ := ConfigDir(); got != filepath.Join(home, ".config", "fleet") {
			t.Errorf("ConfigDir = %q", got)
		}
		if got, _ := StateDir(); got != filepath.Join(home, ".local", "state", "fleet") {
			t.Errorf("StateDir = %q", got)
		}
		if got, _ := CacheDir(""); got != filepath.Join(home, ".cache", "fleet") {
			t.Errorf("CacheDir = %q", got)
		}
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want one warning per variable, got:\n%s", buf.String())
	}
	for _, l := range lines {
		if !strings.Contains(l, ": warning: ignoring XDG_") {
			t.Errorf("unexpected line %q", l)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := map[string]string{
		"~":       home,
		"~/a/b":   filepath.Join(home, "a/b"),
		"/abs":    "/abs",
		"rel":     "rel",
		"~other/": "~other/",
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}
