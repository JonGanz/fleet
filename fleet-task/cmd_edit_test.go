package main

import (
	"reflect"
	"testing"
)

func TestAvailableRepoNames(t *testing.T) {
	cfg := &ReposConfig{
		Repos: []RepoConfig{
			{Name: "backend"},
			{Name: "admin-ui"},
			{Name: "worker"},
		},
	}
	attached := []TaskRepo{
		{Repo: "admin-ui"},
	}

	got := availableRepoNames(cfg, attached)
	want := []string{"backend", "worker"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("availableRepoNames() = %v, want %v", got, want)
	}
}

func TestAvailableRepoNamesNoneAttached(t *testing.T) {
	cfg := &ReposConfig{
		Repos: []RepoConfig{
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

func TestAvailableRepoNamesAllAttached(t *testing.T) {
	cfg := &ReposConfig{
		Repos: []RepoConfig{
			{Name: "backend"},
			{Name: "admin-ui"},
		},
	}
	attached := []TaskRepo{
		{Repo: "backend"},
		{Repo: "admin-ui"},
	}

	got := availableRepoNames(cfg, attached)
	if len(got) != 0 {
		t.Errorf("availableRepoNames() = %v, want empty", got)
	}
}
