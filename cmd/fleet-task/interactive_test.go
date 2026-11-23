package main

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
)

func TestListFlag(t *testing.T) {
	var l listFlag
	for _, v := range []string{"a,b", " c ", ",,", "d"} {
		l.Set(v)
	}
	if want := (listFlag{"a", "b", "c", "d"}); !reflect.DeepEqual(l, want) {
		t.Errorf("listFlag = %v, want %v", l, want)
	}
}

func TestNeedInputIsUsageError(t *testing.T) {
	if !cli.IsUsage(fmt.Errorf("context: %w", needInput("x", "pass y"))) {
		t.Error("wrapped usage error not detected")
	}
	if cli.IsUsage(errors.New("plain")) {
		t.Error("plain error misclassified as usage error")
	}
}

func TestParseCmdArgsErrorsAreUsageErrors(t *testing.T) {
	if _, err := parseCmdArgs(specShow, []string{"a", "b"}, nil); !cli.IsUsage(err) {
		t.Errorf("too many args: %v", err)
	}
	if _, err := parseCmdArgs(specShow, []string{"--bogus"}, nil); !cli.IsUsage(err) {
		t.Errorf("unknown flag: %v", err)
	}
}

func TestTrackedString(t *testing.T) {
	var ts trackedString
	if ts.set {
		t.Fatal("unset trackedString reports set")
	}
	ts.Set("")
	if !ts.set || ts.value != "" {
		t.Errorf("explicit empty value: %+v", ts)
	}
}

func TestUniqueStrings(t *testing.T) {
	if got := uniqueStrings([]string{"b", "a", "b", "c", "a"}); !reflect.DeepEqual(got, []string{"b", "a", "c"}) {
		t.Errorf("uniqueStrings = %v", got)
	}
}
