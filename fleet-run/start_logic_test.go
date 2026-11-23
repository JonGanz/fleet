package main

import (
	"errors"
	"testing"
)

func testPairs() []RunPair {
	return []RunPair{
		{Repo: "backend", RunName: "api", Cmd: "npm start", Runtime: "linux", WorktreeDir: "/wt/backend"},
		{Repo: "web", RunName: "dev", Cmd: "npm run dev", Runtime: "linux", WorktreeDir: "/wt/web"},
	}
}

func isUsage(err error) bool {
	var ue *usageError
	return errors.As(err, &ue)
}

func TestExistingWindowStatus(t *testing.T) {
	if got := existingWindowStatus(PaneInfo{Alive: true}); got != startStatusAlreadyRunning {
		t.Errorf("live = %q", got)
	}
	if got := existingWindowStatus(PaneInfo{Alive: false}); got != startStatusRespawned {
		t.Errorf("dead = %q", got)
	}
}

func TestResolveStartTargetsAll(t *testing.T) {
	got, needPicker, err := resolveStartTargets(testPairs(), true, nil)
	if err != nil || needPicker || len(got) != 2 {
		t.Errorf("got %v %v %v", got, needPicker, err)
	}
}

func TestResolveStartTargetsExplicitDedupes(t *testing.T) {
	got, needPicker, err := resolveStartTargets(testPairs(), false, []string{"web:dev", "web:dev", "backend:api"})
	if err != nil || needPicker {
		t.Fatalf("err=%v needPicker=%v", err, needPicker)
	}
	if len(got) != 2 || got[0].Label() != "web:dev" || got[1].Label() != "backend:api" {
		t.Errorf("got %v", got)
	}
}

func TestResolveStartTargetsNeedsPicker(t *testing.T) {
	_, needPicker, err := resolveStartTargets(testPairs(), false, nil)
	if err != nil || !needPicker {
		t.Errorf("needPicker=%v err=%v", needPicker, err)
	}
}

func TestResolveStartTargetsUsageErrors(t *testing.T) {
	cases := []struct {
		all    bool
		labels []string
	}{
		{true, []string{"web:dev"}},
		{false, []string{"nocolon"}},
		{false, []string{"web:nope"}},
	}
	for _, c := range cases {
		if _, _, err := resolveStartTargets(testPairs(), c.all, c.labels); !isUsage(err) {
			t.Errorf("all=%v labels=%v: want usage error, got %v", c.all, c.labels, err)
		}
	}
}

func TestStartReportFailedCount(t *testing.T) {
	r := StartReport{Results: []StartResult{{Status: startStatusStarted}, {Status: startStatusFailed}, {Status: startStatusRespawned}}}
	if r.failedCount() != 1 {
		t.Errorf("failedCount = %d", r.failedCount())
	}
}
