package main

import (
	"path/filepath"
	"testing"
)

func TestCacheRootPrecedence(t *testing.T) {
	t.Setenv("FLEET_CACHE_DIR", "/env/fleet")
	t.Setenv("XDG_CACHE_HOME", "/xdg")

	if got, _ := cacheRoot("/flag"); got != "/flag" {
		t.Errorf("flag should win, got %q", got)
	}
	if got, _ := cacheRoot(""); got != "/env/fleet" {
		t.Errorf("FLEET_CACHE_DIR should win over XDG, got %q", got)
	}
	t.Setenv("FLEET_CACHE_DIR", "")
	if got, _ := cacheRoot(""); got != filepath.Join("/xdg", "fleet") {
		t.Errorf("XDG fallback, got %q", got)
	}
}
