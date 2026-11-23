package main

import "testing"

func TestWindowName(t *testing.T) {
	got := windowName("backend", "api")
	want := "backend-api"
	if got != want {
		t.Errorf("windowName = %q, want %q", got, want)
	}
}
