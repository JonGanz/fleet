package main

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromSettings(t *testing.T) {
	rev := []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}}
	cases := []struct {
		settings []debug.BuildSetting
		want     string
	}{
		{nil, "dev"},
		{rev, "dev-0123456789ab"},
		{append(rev, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "dev-0123456789ab-dirty"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "dev-abc"},
	}
	for _, c := range cases {
		if got := versionFromSettings("dev", c.settings); got != c.want {
			t.Errorf("versionFromSettings(%v) = %q, want %q", c.settings, got, c.want)
		}
	}
}

func TestVersionStringStamped(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "1.2.3"
	if got := versionString(); got != "1.2.3" {
		t.Errorf("got %q", got)
	}
}
