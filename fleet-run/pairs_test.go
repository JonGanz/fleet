package main

import (
	"reflect"
	"testing"
)

func TestAvailablePairs(t *testing.T) {
	cfg, err := parseReposConfig([]byte(reposYAMLFixture))
	if err != nil {
		t.Fatalf("parseReposConfig: %v", err)
	}

	ts := &TaskState{
		Ticket:      "PROJ-1234",
		Description: "Add retry logic to payment webhook",
		Repos: []TaskRepo{
			{Repo: "backend", Branch: "PROJ-1234", WorktreePath: "/state/worktrees/PROJ-1234/backend"},
			{Repo: "admin-ui", Branch: "PROJ-1234", WorktreePath: "/state/worktrees/PROJ-1234/admin-ui"},
		},
	}

	got := availablePairs(cfg, ts)
	want := []RunPair{
		{Repo: "backend", RunName: "api", Cmd: "npm run start:dev", Runtime: "linux", WorktreeDir: "/state/worktrees/PROJ-1234/backend"},
		{Repo: "backend", RunName: "worker", Cmd: "npm run start:worker", Runtime: "linux", WorktreeDir: "/state/worktrees/PROJ-1234/backend"},
		{Repo: "admin-ui", RunName: "dev", Cmd: "npm run dev", Runtime: "windows", WorktreeDir: "/state/worktrees/PROJ-1234/admin-ui"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("availablePairs = %+v, want %+v", got, want)
	}

	if got := got[2].Label(); got != "admin-ui:dev" {
		t.Errorf("Label() = %q, want admin-ui:dev", got)
	}
	if got := want[0].WindowName(); got != "backend-api" {
		t.Errorf("WindowName = %q, want backend-api", got)
	}
}

func TestAvailablePairsSkipsReposNotInRepoConfig(t *testing.T) {
	cfg, err := parseReposConfig([]byte(reposYAMLFixture))
	if err != nil {
		t.Fatalf("parseReposConfig: %v", err)
	}
	ts := &TaskState{
		Ticket: "PROJ-1234",
		Repos: []TaskRepo{
			{Repo: "backend", WorktreePath: "/state/worktrees/PROJ-1234/backend"},
			{Repo: "removed-repo", WorktreePath: "/state/worktrees/PROJ-1234/removed-repo"},
		},
	}
	got := availablePairs(cfg, ts)
	if len(got) != 2 {
		t.Fatalf("len(availablePairs) = %d, want 2 (only backend's two run targets)", len(got))
	}
	for _, p := range got {
		if p.Repo != "backend" {
			t.Errorf("unexpected pair for repo %q; removed-repo should have been skipped", p.Repo)
		}
	}
}

func TestFindPairByLabel(t *testing.T) {
	pairs := []RunPair{
		{Repo: "backend", RunName: "api"},
		{Repo: "backend", RunName: "worker"},
	}
	got, ok := findPairByLabel(pairs, "backend:worker")
	if !ok || got.RunName != "worker" {
		t.Errorf("findPairByLabel(backend:worker) = %+v, %v", got, ok)
	}
	if _, ok := findPairByLabel(pairs, "backend:nope"); ok {
		t.Error("findPairByLabel(backend:nope) found unexpectedly")
	}
}
