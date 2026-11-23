package tmuxsock

import (
	"reflect"
	"testing"
)

func TestArgs(t *testing.T) {
	t.Setenv(Env, "")
	if got := Args("show-options", "-g"); !reflect.DeepEqual(got, []string{"show-options", "-g"}) {
		t.Errorf("no socket: %v", got)
	}
	t.Setenv(Env, "fleet-test")
	if got := Args("show-options", "-g"); !reflect.DeepEqual(got, []string{"-L", "fleet-test", "show-options", "-g"}) {
		t.Errorf("socket: %v", got)
	}
}

func TestArgv(t *testing.T) {
	args := []string{"-f", "/x.conf", "new-session"}
	if got := Argv("", args); !reflect.DeepEqual(got, args) {
		t.Errorf("no socket: %v", got)
	}
	want := []string{"-L", "priv", "-f", "/x.conf", "new-session"}
	if got := Argv("priv", args); !reflect.DeepEqual(got, want) {
		t.Errorf("socket: %v, want %v", got, want)
	}
}
