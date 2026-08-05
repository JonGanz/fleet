package main

import (
	"fmt"
	"os"
	"path"

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
	WindowsBase   string      `yaml:"windows_base"`
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

// DefaultsConfig holds fleet-wide fallback values for per-repo fields, so
// repos.yaml doesn't need to repeat default_branch/base on every entry.
type DefaultsConfig struct {
	DefaultBranch   string `yaml:"default_branch"`
	BaseRoot        string `yaml:"base_root"`
	WindowsBaseRoot string `yaml:"windows_base_root"`
}

// ReposConfig is the top-level shape of repos.yaml.
type ReposConfig struct {
	Tmux                TmuxConfig     `yaml:"tmux"`
	WorktreeRoot        string         `yaml:"worktree_root"`
	WindowsWorktreeRoot string         `yaml:"windows_worktree_root"`
	WindowsCacheRoot    string         `yaml:"windows_cache_root"`
	Defaults            DefaultsConfig `yaml:"defaults"`
	Repos               []RepoConfig   `yaml:"repos"`
}

// applyDefaults fills any repo field left blank in repos.yaml with the
// top-level `defaults` values: default_branch falls back directly, base
// falls back to <base_root>/<repo-name> when the repo omits `base`, and for
// runtime: windows repos, windows_base falls back to
// <windows_base_root>/<repo-name> the same way.
func (c *ReposConfig) applyDefaults() {
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.DefaultBranch == "" {
			r.DefaultBranch = c.Defaults.DefaultBranch
		}
		if r.Runtime != "windows" && r.Base == "" && c.Defaults.BaseRoot != "" {
			r.Base = path.Join(c.Defaults.BaseRoot, r.Name)
		}
		if r.Runtime == "windows" && r.WindowsBase == "" && c.Defaults.WindowsBaseRoot != "" {
			r.WindowsBase = path.Join(c.Defaults.WindowsBaseRoot, r.Name)
		}
	}
}

// loadReposConfig reads and parses repos.yaml from the given path.
func loadReposConfig(configPath string) (*ReposConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read repos config %s: %w", configPath, err)
	}
	cfg, err := parseReposConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse repos config %s: %w", configPath, err)
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
	cfg.applyDefaults()
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
