package main

import (
	"reflect"
	"testing"
)

func TestParseFlagsInterspersed(t *testing.T) {
	fs := newFlagSet("stop")
	all := fs.Bool("all", false, "")
	ticket := fs.String("ticket", "", "")
	pos, err := parseFlags(fs, "usage", []string{"backend:api", "--all", "web:dev", "--ticket", "A-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !*all || *ticket != "A-1" {
		t.Errorf("all=%v ticket=%q", *all, *ticket)
	}
	if want := []string{"backend:api", "web:dev"}; !reflect.DeepEqual(pos, want) {
		t.Errorf("pos = %v, want %v", pos, want)
	}
}

func TestParseFlagsDoubleDash(t *testing.T) {
	fs := newFlagSet("stop")
	all := fs.Bool("all", false, "")
	pos, err := parseFlags(fs, "usage", []string{"--", "--all", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if *all {
		t.Error("--all after -- should be positional")
	}
	if want := []string{"--all", "x"}; !reflect.DeepEqual(pos, want) {
		t.Errorf("pos = %v, want %v", pos, want)
	}
}

func TestParseFlagsUnknown(t *testing.T) {
	fs := newFlagSet("start")
	if _, err := parseFlags(fs, "usage", []string{"--bogus"}); err == nil {
		t.Error("expected error for unknown flag")
	}
}
