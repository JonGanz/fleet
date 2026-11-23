package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"runtime/debug"
	"testing"
)

func TestDiagnosticPrefix(t *testing.T) {
	var buf bytes.Buffer
	restore := SetStderr(&buf)
	defer restore()
	old := Program()
	SetProgram("fleet-test")
	defer SetProgram(old)

	Errorf("no task found for ticket %s", "X-1")
	Warnf("skipping %s\n", "f.json")
	want := "fleet-test: error: no task found for ticket X-1\nfleet-test: warning: skipping f.json\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}

	buf.Reset()
	if code := Report(Usagef("bad flag")); code != ExitUsage || buf.String() != "fleet-test: error: bad flag\n" {
		t.Errorf("Report = %d, printed %q", code, buf.String())
	}
	buf.Reset()
	if code := Report(flag.ErrHelp); code != ExitOK || buf.Len() != 0 {
		t.Errorf("Report(ErrHelp) = %d, printed %q", code, buf.String())
	}

	var own bytes.Buffer
	FErrorf(&own, "x")
	if own.String() != "fleet-test: error: x\n" {
		t.Errorf("FErrorf wrote %q", own.String())
	}
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, ExitOK},
		{flag.ErrHelp, ExitOK},
		{errors.New("boom"), ExitFailure},
		{Usagef("bad"), ExitUsage},
		{fmt.Errorf("wrapped: %w", Usagef("pass --all")), ExitUsage},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("ExitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestInteractiveAllowedHonorsNoInputEnv(t *testing.T) {
	t.Setenv("FLEET_NO_INPUT", "1")
	if InteractiveAllowed() {
		t.Error("FLEET_NO_INPUT=1 must disable interaction")
	}
}

func TestInteractiveFrom(t *testing.T) {
	cases := []struct {
		tty     bool
		noInput string
		want    bool
	}{
		{true, "", true},
		{true, "0", true},
		{true, "1", false},
		{false, "", false},
	}
	for _, c := range cases {
		if got := interactiveFrom(c.tty, c.noInput); got != c.want {
			t.Errorf("interactiveFrom(%v, %q) = %v, want %v", c.tty, c.noInput, got, c.want)
		}
	}
}

func TestIsUsageWrapped(t *testing.T) {
	if !IsUsage(fmt.Errorf("context: %w", Usagef("missing %s", "x"))) {
		t.Error("wrapped usage error not detected")
	}
	if IsUsage(errors.New("plain")) {
		t.Error("plain error misclassified as usage error")
	}
}

func TestResolveVersion(t *testing.T) {
	vcs := func(rev, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main:     debug.Module{Version: "(devel)"},
			Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}},
		}
	}
	cases := []struct {
		name     string
		ldflags  string
		info     *debug.BuildInfo
		ok       bool
		expected string
	}{
		{"ldflags wins", "v1.2.3", vcs("0123456789abcdef", "false"), true, "v1.2.3"},
		{"module version", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}}, true, "v0.4.0"},
		{"vcs revision", "dev", vcs("0123456789abcdef", "false"), true, "0123456789ab"},
		{"dirty tree", "dev", vcs("0123456789abcdef0123", "true"), true, "0123456789ab-dirty"},
		{"short revision", "dev", vcs("abc", "false"), true, "abc"},
		{"no vcs info", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
		{"empty build info", "dev", &debug.BuildInfo{}, true, "dev"},
		{"no build info", "dev", nil, false, "dev"},
		{"empty ldflags", "", nil, false, "dev"},
	}
	for _, c := range cases {
		if got := resolveVersion(c.ldflags, c.info, c.ok); got != c.expected {
			t.Errorf("%s: got %q, want %q", c.name, got, c.expected)
		}
	}
}

func TestVersionStamped(t *testing.T) {
	if got := Version("1.2.3"); got != "1.2.3" {
		t.Errorf("Version(stamped) = %q", got)
	}
}
