package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatStatusEmpty(t *testing.T) {
	got, err := formatStatus(Status{Session: "fleet"}, false)
	if err != nil || got != "nothing running" {
		t.Errorf("got %q, %v", got, err)
	}
	js, err := formatStatus(Status{Session: "fleet"}, true)
	if err != nil {
		t.Fatal(err)
	}
	var back Status
	if err := json.Unmarshal([]byte(js), &back); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `"windows": []`) {
		t.Errorf("windows should be an empty array, got %s", js)
	}
}

func TestFormatStatusRunning(t *testing.T) {
	st := Status{Session: "fleet", ActiveTicket: "A-1", Description: "do it", Windows: []string{"backend-api", "admin-ui-dev"}}
	txt, err := formatStatus(st, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"A-1", "do it", "backend-api", "admin-ui-dev"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text missing %q: %s", want, txt)
		}
	}
	js, _ := formatStatus(st, true)
	var back Status
	if err := json.Unmarshal([]byte(js), &back); err != nil || back.ActiveTicket != "A-1" || len(back.Windows) != 2 {
		t.Errorf("json round trip: %v %+v", err, back)
	}
}

func TestFormatStatusMarksDeadPanes(t *testing.T) {
	code := 3
	st := Status{
		Session: "fleet", ActiveTicket: "A-1", Windows: []string{"backend-api", "web-dev"},
		Panes: []PaneInfo{{Window: "backend-api", Alive: true}, {Window: "web-dev", ExitStatus: &code}},
	}
	txt, err := formatStatus(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt, "web-dev  (dead, exit 3") || strings.Contains(txt, "backend-api  (dead") {
		t.Errorf("text = %s", txt)
	}
	js, _ := formatStatus(Status{Session: "fleet"}, true)
	if !strings.Contains(js, `"panes": []`) {
		t.Errorf("panes should be an empty array, got %s", js)
	}
}

func TestFormatStatusStale(t *testing.T) {
	if !isStaleActive("A-1", 0) || isStaleActive("A-1", 1) || isStaleActive("", 0) {
		t.Error("isStaleActive wrong")
	}
	st := Status{Session: "fleet", ActiveTicket: "A-1", StaleActive: true}
	txt, err := formatStatus(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt, "STALE") || !strings.Contains(txt, "fleet-run stop") {
		t.Errorf("text = %s", txt)
	}
	js, _ := formatStatus(st, true)
	if !strings.Contains(js, `"stale_active": true`) {
		t.Errorf("json = %s", js)
	}
}
