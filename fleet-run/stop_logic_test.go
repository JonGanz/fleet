package main

import (
	"reflect"
	"testing"
)

func TestValidateAllFlags(t *testing.T) {
	if err := validateAllFlags("", false); err == nil {
		t.Error("expected error for --all with no ticket and no --everything")
	}
	if err := validateAllFlags("PROJ-1234", false); err != nil {
		t.Errorf("unexpected error for --all --ticket: %v", err)
	}
	if err := validateAllFlags("", true); err != nil {
		t.Errorf("unexpected error for --all --everything: %v", err)
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
	got, err := namesToWindowNames([]string{"backend:api", "admin-ui:dev"}, "PROJ-1234")
	if err != nil {
		t.Fatalf("namesToWindowNames: %v", err)
	}
	want := []string{"PROJ-1234-backend-api", "PROJ-1234-admin-ui-dev"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("namesToWindowNames = %v, want %v", got, want)
	}

	if _, err := namesToWindowNames([]string{"bad"}, "PROJ-1234"); err == nil {
		t.Error("expected error for malformed name")
	}
}

func TestFilterAllTarget(t *testing.T) {
	windows := []string{
		"PROJ-1234-backend-api",
		"PROJ-1234-admin-ui-dev",
		"PROJ-9999-backend-api",
	}

	got, err := filterAllTarget(windows, "PROJ-1234", false)
	if err != nil {
		t.Fatalf("filterAllTarget: %v", err)
	}
	want := []string{"PROJ-1234-backend-api", "PROJ-1234-admin-ui-dev"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterAllTarget = %v, want %v", got, want)
	}

	if _, err := filterAllTarget(windows, "", false); err == nil {
		t.Error("expected error for --all with no ticket and no --everything")
	}

	all, err := filterAllTarget(windows, "", true)
	if err != nil {
		t.Fatalf("filterAllTarget with --everything: %v", err)
	}
	if !reflect.DeepEqual(all, windows) {
		t.Errorf("filterAllTarget(--everything) = %v, want all windows %v", all, windows)
	}
}

func TestCandidateWindowsForStop(t *testing.T) {
	windows := []string{
		"PROJ-1234-backend-api",
		"PROJ-9999-backend-api",
	}
	got := candidateWindowsForStop(windows, "PROJ-1234")
	want := []string{"PROJ-1234-backend-api"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidateWindowsForStop = %v, want %v", got, want)
	}

	all := candidateWindowsForStop(windows, "")
	if !reflect.DeepEqual(all, windows) {
		t.Errorf("candidateWindowsForStop(empty ticket) = %v, want %v", all, windows)
	}
}
