package main

import (
	"reflect"
	"testing"
)

func TestCheckTicketMatchesActive(t *testing.T) {
	if err := checkTicketMatchesActive("PROJ-1234", "", false); err == nil {
		t.Error("expected error when no ticket is active")
	}
	if err := checkTicketMatchesActive("PROJ-1234", "PROJ-9999", true); err == nil {
		t.Error("expected error when a different ticket is active")
	}
	if err := checkTicketMatchesActive("PROJ-1234", "PROJ-1234", true); err != nil {
		t.Errorf("unexpected error when the given ticket matches the active one: %v", err)
	}
}

func TestParsePairLabel(t *testing.T) {
	repo, run, ok := parsePairLabel("backend:api")
	if !ok || repo != "backend" || run != "api" {
		t.Errorf("parsePairLabel(backend:api) = %q, %q, %v", repo, run, ok)
	}
	if _, _, ok := parsePairLabel("no-colon"); ok {
		t.Error("parsePairLabel(no-colon) should fail")
	}
	if _, _, ok := parsePairLabel(":api"); ok {
		t.Error("parsePairLabel(:api) should fail (empty repo)")
	}
	if _, _, ok := parsePairLabel("backend:"); ok {
		t.Error("parsePairLabel(backend:) should fail (empty run name)")
	}
}

func TestNamesToWindowNames(t *testing.T) {
	got, err := namesToWindowNames([]string{"backend:api", "admin-ui:dev"})
	if err != nil {
		t.Fatalf("namesToWindowNames: %v", err)
	}
	want := []string{"backend-api", "admin-ui-dev"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("namesToWindowNames = %v, want %v", got, want)
	}

	if _, err := namesToWindowNames([]string{"bad"}); err == nil {
		t.Error("expected error for malformed name")
	}
}
