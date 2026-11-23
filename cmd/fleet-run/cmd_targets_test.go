package main

import "testing"

func TestBuildTargets(t *testing.T) {
	panes := []PaneInfo{{Window: "backend-api", Alive: true}, {Window: "web-dev", Alive: false}}

	got := buildTargets(testPairs(), panes, true)
	if len(got) != 2 || got[0].Label != "backend:api" || got[0].Target != "backend:api" || got[0].Window != "backend-api" || got[0].Cwd != "/wt/backend" {
		t.Fatalf("got %+v", got)
	}
	if !got[0].Running || got[1].Running {
		t.Errorf("running flags wrong: %+v", got)
	}

	// Same window names belonging to another (active) ticket never count.
	for _, ti := range buildTargets(testPairs(), panes, false) {
		if ti.Running {
			t.Errorf("%s marked running for an inactive ticket", ti.Label)
		}
	}
}
