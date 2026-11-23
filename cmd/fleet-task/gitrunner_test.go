package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func refSet(refs ...string) func(string) (bool, error) {
	set := map[string]bool{}
	for _, r := range refs {
		set[r] = true
	}
	return func(ref string) (bool, error) { return set[ref], nil }
}

func TestPlanBaseSetup(t *testing.T) {
	const url = "git@example.com:o/r.git"
	cases := []struct {
		name    string
		info    baseInfo
		origin  string
		want    baseSetup
		wantErr bool
		warn    bool
	}{
		{"missing", baseInfo{}, url, baseSetup{Init: true, AddOrigin: true}, false, false},
		{"missing no origin", baseInfo{}, "", baseSetup{}, true, false},
		{"empty dir", baseInfo{Exists: true, Empty: true}, url, baseSetup{Init: true, AddOrigin: true}, false, false},
		{"non-empty non-repo", baseInfo{Exists: true}, url, baseSetup{}, true, false},
		{"repo without origin", baseInfo{Exists: true, IsRepo: true}, url, baseSetup{AddOrigin: true}, false, false},
		{"repo ok", baseInfo{Exists: true, IsRepo: true, OriginURL: url}, url, baseSetup{}, false, false},
		{"repo other origin", baseInfo{Exists: true, IsRepo: true, OriginURL: "https://x/y"}, url, baseSetup{}, false, true},
	}
	for _, c := range cases {
		got, err := planBaseSetup(c.info, "r", "/base", c.origin)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
			continue
		}
		if got.Init != c.want.Init || got.AddOrigin != c.want.AddOrigin || (got.Warning != "") != c.warn {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}

func TestPlanWorktreeAdd(t *testing.T) {
	cases := []struct {
		name    string
		refs    []string
		want    worktreeAddPlan
		wantErr string
	}{
		{"local wins", []string{"refs/heads/T-1", "refs/remotes/origin/T-1", "refs/remotes/origin/main"},
			worktreeAddPlan{Mode: worktreeAttach, Branch: "T-1"}, ""},
		{"remote branch tracked", []string{"refs/remotes/origin/T-1", "refs/remotes/origin/main"},
			worktreeAddPlan{Mode: worktreeTrack, Branch: "T-1", StartPoint: "origin/T-1"}, ""},
		{"fresh from default", []string{"refs/remotes/origin/main"},
			worktreeAddPlan{Mode: worktreeCreate, Branch: "T-1", StartPoint: "origin/main"}, ""},
		{"typo'd default", []string{"refs/remotes/origin/main"}, worktreeAddPlan{}, "origin/mian does not exist"},
	}
	for _, c := range cases {
		def := "main"
		if strings.Contains(c.name, "typo") {
			def = "mian"
		}
		got, err := planWorktreeAdd("T-1", def, refSet(c.refs...))
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err = %v, want containing %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}

	if _, err := planWorktreeAdd("T-1", "", refSet()); err == nil || !strings.Contains(err.Error(), "default_branch") {
		t.Errorf("no default branch: %v", err)
	}
	boom := func(string) (bool, error) { return false, errors.New("boom") }
	if _, err := planWorktreeAdd("T-1", "main", boom); err == nil {
		t.Error("probe errors must propagate")
	}
}

func TestWorktreeAddPlanArgs(t *testing.T) {
	cases := []struct {
		plan worktreeAddPlan
		want []string
	}{
		{worktreeAddPlan{Mode: worktreeAttach, Branch: "b"}, []string{"worktree", "add", "/wt", "b"}},
		{worktreeAddPlan{Mode: worktreeTrack, Branch: "b", StartPoint: "origin/b"}, []string{"worktree", "add", "--track", "-b", "b", "/wt", "origin/b"}},
		{worktreeAddPlan{Mode: worktreeCreate, Branch: "b", StartPoint: "origin/main"}, []string{"worktree", "add", "--no-track", "-b", "b", "/wt", "origin/main"}},
	}
	for _, c := range cases {
		if got := c.plan.args("/wt"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: args = %v, want %v", c.plan.Mode, got, c.want)
		}
	}
}
