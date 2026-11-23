package main

import (
	"reflect"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
)

func TestEnvWithout(t *testing.T) {
	got := envWithout([]string{"A=1", "TMUX=/x,1,0", "TMUX_PANE=%1", "B=2"}, "TMUX")
	if want := []string{"A=1", "TMUX_PANE=%1", "B=2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInsideServer(t *testing.T) {
	cases := []struct {
		env, socket string
		want        bool
	}{
		{"", "/tmp/tmux-1000/default", false},
		{"/tmp/tmux-1000/default,123,0", "", false},
		{"/tmp/tmux-1000/default,123,0", "/tmp/tmux-1000/default", true},
		{"/tmp/tmux-1000/default,123,0", "/tmp/tmux-1000/private", false},
	}
	for _, c := range cases {
		if got := insideServer(c.env, c.socket); got != c.want {
			t.Errorf("insideServer(%q, %q) = %v, want %v", c.env, c.socket, got, c.want)
		}
	}
}

func TestRunAttachNeedsTerminal(t *testing.T) {
	t.Setenv(cli.NoInputEnv, "1")
	if err := runAttach(""); cli.ExitCode(err) != cli.ExitUsage {
		t.Errorf("err = %v, want a usage error", err)
	}
	if err := runAttach("no-colon"); cli.ExitCode(err) != cli.ExitUsage {
		t.Errorf("malformed target: err = %v, want a usage error", err)
	}
}
