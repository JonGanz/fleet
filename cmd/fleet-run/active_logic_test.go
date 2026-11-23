package main

import "testing"

func TestNeedsSwitch(t *testing.T) {
	tests := []struct {
		active    string
		hasActive bool
		ticket    string
		want      bool
	}{
		{"", false, "A-1", false},
		{"A-1", true, "A-1", false},
		{"A-1", true, "B-2", true},
		{"stale", false, "B-2", false},
	}
	for _, tt := range tests {
		if got := needsSwitch(tt.active, tt.hasActive, tt.ticket); got != tt.want {
			t.Errorf("needsSwitch(%q,%v,%q) = %v, want %v", tt.active, tt.hasActive, tt.ticket, got, tt.want)
		}
	}
}

func TestShouldClearActive(t *testing.T) {
	if !shouldClearActive(nil) || !shouldClearActive([]string{}) {
		t.Error("expected clear when nothing remains")
	}
	if shouldClearActive([]string{"backend-api"}) {
		t.Error("expected no clear when windows remain")
	}
}
