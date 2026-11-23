package main

import "testing"

func TestIsSSHURL(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"git@github.com:org/backend.git", true},
		{"ssh://git@github.com/org/backend.git", true},
		{"https://github.com/org/backend.git", false},
		{"http://github.com/org/backend.git", false},
		{"/local/path/to/repo", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isSSHURL(c.origin); got != c.want {
			t.Errorf("isSSHURL(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}
