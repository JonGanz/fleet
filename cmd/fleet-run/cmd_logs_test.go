package main

import (
	"reflect"
	"testing"
)

func TestLastLines(t *testing.T) {
	got := lastLines("a\nb\nc\n\n   \n", 2)
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("got %q", got)
	}
	got = lastLines("a\nb\n", 10)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %q", got)
	}
	if got := lastLines("\n\n", 5); len(got) != 0 {
		t.Errorf("blank capture should give no lines, got %q", got)
	}
}

func TestLastLinesCollapsesDeadPanePadding(t *testing.T) {
	got := lastLines("boom\n\n\n\nPane is dead (status 7, now)\n\n", 2)
	want := []string{"boom", "Pane is dead (status 7, now)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
