package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// RunConfig is one named run command for a repo, e.g.
//
//	run:
//	  - name: api
//	    cmd: "npm run start:dev"
type RunConfig struct {
	Name string `yaml:"name"`
	Cmd  string `yaml:"cmd"`
}

// RepoConfig describes one repo entry in repos.yaml.
type RepoConfig struct {
	Name          string      `yaml:"name"`
	Origin        string      `yaml:"origin"`
	Base          string      `yaml:"base"`
	DefaultBranch string      `yaml:"default_branch"`
	Runtime       string      `yaml:"runtime"` // "linux" (default) or "windows"
	Run           []RunConfig `yaml:"run"`
}

// IsWindows reports whether this repo's run commands must be executed via
// powershell.exe from WSL2 rather than directly in the WSL Linux shell.
func (r *RepoConfig) IsWindows() bool {
	return r.Runtime == "windows"
}

// TmuxConfig is the tmux section of repos.yaml.
type TmuxConfig struct {
	SessionName string `yaml:"session_name"`
	ConfigFile  string `yaml:"config_file"`
}

// ReposConfig is the top-level shape of repos.yaml.
type ReposConfig struct {
	Tmux         TmuxConfig   `yaml:"tmux"`
	WorktreeRoot string       `yaml:"worktree_root"`
	Repos        []RepoConfig `yaml:"repos"`
}

// loadReposConfig reads and parses repos.yaml from the given path.
func loadReposConfig(path string) (*ReposConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read repos config %s: %w", path, err)
	}
	cfg, err := parseReposConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse repos config %s: %w", path, err)
	}
	return cfg, nil
}

// parseReposConfig parses repos.yaml content already read into memory. Split
// out from loadReposConfig so tests can exercise parsing without touching
// disk.
func parseReposConfig(data []byte) (*ReposConfig, error) {
	var cfg ReposConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// findRepo returns the RepoConfig with the given name, or nil if not found.
func (c *ReposConfig) findRepo(name string) *RepoConfig {
	if c == nil {
		return nil
	}
	for i := range c.Repos {
		if c.Repos[i].Name == name {
			return &c.Repos[i]
		}
	}
	return nil
}
