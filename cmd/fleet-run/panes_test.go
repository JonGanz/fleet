package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLabelPanes(t *testing.T) {
	got := labelPanes([]PaneInfo{{Window: "backend-api"}, {Window: "orphan"}}, map[string]string{"backend-api": "backend:api"})
	if got[0].Target != "backend:api" || got[1].Target != "" {
		t.Errorf("got %+v", got)
	}
}

func TestPaneInfoJSONOmitsExitStatusWhenAlive(t *testing.T) {
	b, _ := json.Marshal(PaneInfo{WindowID: "@1", Window: "w", Alive: true, PID: 1})
	if strings.Contains(string(b), "exit_status") || strings.Contains(string(b), "@1") {
		t.Errorf("live pane JSON should omit exit_status: %s", b)
	}
}

func TestParsePaneLineDottedName(t *testing.T) {
	p, err := parsePaneLine("@9\tmy.app-dev\t1\t9\t0")
	if err != nil || p.WindowID != "@9" || p.Window != "my.app-dev" || p.Alive {
		t.Errorf("got %+v, %v", p, err)
	}
}

func TestParsePaneLineRejectsGarbage(t *testing.T) {
	for _, line := range []string{"onlyname", "@1\tw\tx\t1\t", "@1\tw\t0\tpid\t", "@1\tw\t1\t1\tbad", "w\t0\t1\t", "noid\tw\t0\t1\t"} {
		if _, err := parsePaneLine(line); err == nil {
			t.Errorf("parsePaneLine(%q) should fail", line)
		}
	}
}

func TestParsePaneLines(t *testing.T) {
	out := "@1\tbackend-api\t0\t123\t\n@2\tadmin-ui-dev\t1\t456\t3\n\n"
	panes, err := parsePaneLines(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 2 {
		t.Fatalf("got %d panes", len(panes))
	}
	if p := panes[0]; p.WindowID != "@1" || p.Window != "backend-api" || !p.Alive || p.PID != 123 || p.ExitStatus != nil {
		t.Errorf("live pane = %+v", p)
	}
	if p := panes[1]; p.Window != "admin-ui-dev" || p.Alive || p.PID != 456 || p.ExitStatus == nil || *p.ExitStatus != 3 {
		t.Errorf("dead pane = %+v", p)
	}
}

func TestParsePaneLineTabInName(t *testing.T) {
	p, err := parsePaneLine("@4\todd\tname\t0\t9\t")
	if err != nil || p.WindowID != "@4" || p.Window != "odd\tname" || p.PID != 9 {
		t.Errorf("got %+v, %v", p, err)
	}
}
