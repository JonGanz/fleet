package main

import "testing"

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
