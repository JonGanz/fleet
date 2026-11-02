package main

import (
	"errors"
	"flag"
	"reflect"
	"strings"
	"testing"
)

func TestParseCmdArgsInterspersed(t *testing.T) {
	spec := cmdSpec{name: "x", synopsis: "x [a] [b]", maxPos: 2}
	for _, args := range [][]string{
		{"--json", "A", "B"},
		{"A", "--json", "B"},
		{"A", "B", "--json"},
	} {
		var js bool
		pos, err := parseCmdArgs(spec, args, func(fs *flag.FlagSet) { fs.BoolVar(&js, "json", false, "") })
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !js || !reflect.DeepEqual(pos, []string{"A", "B"}) {
			t.Errorf("%v: json=%v pos=%v", args, js, pos)
		}
	}
}

func TestParseCmdArgsDoubleDash(t *testing.T) {
	spec := cmdSpec{name: "x", synopsis: "x [a]", maxPos: 2}
	var js bool
	pos, err := parseCmdArgs(spec, []string{"--", "--json"}, func(fs *flag.FlagSet) { fs.BoolVar(&js, "json", false, "") })
	if err != nil {
		t.Fatal(err)
	}
	if js || !reflect.DeepEqual(pos, []string{"--json"}) {
		t.Errorf("json=%v pos=%v", js, pos)
	}
}

func TestParseCmdArgsErrors(t *testing.T) {
	spec := cmdSpec{name: "rm", synopsis: "rm [ticket]", maxPos: 1}
	if _, err := parseCmdArgs(spec, []string{"A", "B"}, nil); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("too many args: %v", err)
	}
	// An unknown flag (e.g. --json on rm) is an error, never read as a ticket.
	if _, err := parseCmdArgs(spec, []string{"--json"}, nil); err == nil || !strings.Contains(err.Error(), "--help") {
		t.Errorf("unknown flag: %v", err)
	}
}

func TestParseCmdArgsHelp(t *testing.T) {
	spec := cmdSpec{name: "rm", synopsis: "rm [ticket]", maxPos: 1}
	_, err := parseCmdArgs(spec, []string{"--help"}, nil)
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help: %v", err)
	}
	_, err = parseCmdArgs(spec, []string{"-h"}, nil)
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h: %v", err)
	}
}
