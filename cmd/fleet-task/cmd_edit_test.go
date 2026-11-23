package main

import (
	"reflect"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/taskstate"
)

func TestAvailableRepoNames(t *testing.T) {
	cfg := &config.ReposConfig{
		Repos: []config.RepoConfig{
			{Name: "backend"},
			{Name: "admin-ui"},
			{Name: "worker"},
		},
	}
	attached := []taskstate.Repo{
		{Repo: "admin-ui"},
	}

	got := availableRepoNames(cfg, attached)
	want := []string{"backend", "worker"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("availableRepoNames() = %v, want %v", got, want)
	}
}

func TestAvailableRepoNamesAllAttached(t *testing.T) {
	cfg := &config.ReposConfig{
		Repos: []config.RepoConfig{
			{Name: "backend"},
			{Name: "admin-ui"},
		},
	}
	attached := []taskstate.Repo{
		{Repo: "backend"},
		{Repo: "admin-ui"},
	}

	got := availableRepoNames(cfg, attached)
	if len(got) != 0 {
		t.Errorf("availableRepoNames() = %v, want empty", got)
	}
}

func TestAvailableRepoNamesNoneAttached(t *testing.T) {
	cfg := &config.ReposConfig{
		Repos: []config.RepoConfig{
			{Name: "backend"},
			{Name: "admin-ui"},
		},
	}

	got := availableRepoNames(cfg, nil)
	want := []string{"backend", "admin-ui"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("availableRepoNames() = %v, want %v", got, want)
	}
}

func TestCountEditResults(t *testing.T) {
	added, removed, failures := countEditResults([]repoResult{
		{Action: "created", OK: true},
		{Action: "created"},
		{Action: "removed", OK: true},
		{Action: "removed", OK: true},
	})
	if added != 1 || removed != 2 || failures != 1 {
		t.Errorf("got added=%d removed=%d failures=%d", added, removed, failures)
	}
}

func TestValidateEditFlags(t *testing.T) {
	cfg := &config.ReposConfig{Repos: []config.RepoConfig{{Name: "api"}, {Name: "ui"}, {Name: "db"}}}
	st := &taskstate.State{Ticket: "T-1", Repos: []taskstate.Repo{{Repo: "api"}, {Repo: "ui"}}}

	add, remove, err := validateEditFlags(cfg, st, []string{"db", "db"}, []string{"ui"})
	if err != nil {
		t.Fatal(err)
	}
	if len(add) != 1 || add[0] != "db" || len(remove) != 1 || remove[0] != "ui" {
		t.Errorf("got add=%v remove=%v", add, remove)
	}

	bad := []struct{ add, remove []string }{
		{add: []string{"nope"}},
		{add: []string{"api"}},
		{remove: []string{"db"}},
		{add: []string{"db"}, remove: []string{"db"}},
	}
	for _, b := range bad {
		if _, _, err := validateEditFlags(cfg, st, b.add, b.remove); !cli.IsUsage(err) {
			t.Errorf("add=%v remove=%v: err = %v, want usage error", b.add, b.remove, err)
		}
	}
}
