package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePaneLines(t *testing.T) {
	out := "backend-api\t0\t123\t\nadmin-ui-dev\t1\t456\t3\n\n"
	panes, err := parsePaneLines(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 2 {
		t.Fatalf("got %d panes", len(panes))
	}
	if p := panes[0]; p.Window != "backend-api" || !p.Alive || p.PID != 123 || p.ExitStatus != nil {
		t.Errorf("live pane = %+v", p)
	}
	if p := panes[1]; p.Window != "admin-ui-dev" || p.Alive || p.PID != 456 || p.ExitStatus == nil || *p.ExitStatus != 3 {
		t.Errorf("dead pane = %+v", p)
	}
}

func TestParsePaneLineRejectsGarbage(t *testing.T) {
	for _, line := range []string{"onlyname", "w\tx\t1\t", "w\t0\tpid\t", "w\t1\t1\tbad"} {
		if _, err := parsePaneLine(line); err == nil {
			t.Errorf("parsePaneLine(%q) should fail", line)
		}
	}
}

func TestParsePaneLineTabInName(t *testing.T) {
	p, err := parsePaneLine("odd\tname\t0\t9\t")
	if err != nil || p.Window != "odd\tname" || p.PID != 9 {
		t.Errorf("got %+v, %v", p, err)
	}
}

func TestPaneInfoJSONOmitsExitStatusWhenAlive(t *testing.T) {
	b, _ := json.Marshal(PaneInfo{Window: "w", Alive: true, PID: 1})
	if strings.Contains(string(b), "exit_status") {
		t.Errorf("live pane JSON should omit exit_status: %s", b)
	}
}

func TestLabelPanes(t *testing.T) {
	got := labelPanes([]PaneInfo{{Window: "backend-api"}, {Window: "orphan"}}, map[string]string{"backend-api": "backend:api"})
	if got[0].Target != "backend:api" || got[1].Target != "" {
		t.Errorf("got %+v", got)
	}
}
