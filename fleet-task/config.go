package main

import (
	"fmt"
	"os"
	"path"

	"gopkg.in/yaml.v3"
)

// RunConfig is one named run command for a repo (used by fleet-run; parsed
// here only so we round-trip the full repos.yaml shape without loss).
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
	Runtime       string      `yaml:"runtime"`
	Run           []RunConfig `yaml:"run"`
}

// TmuxConfig is the tmux section of repos.yaml (used by fleet-run; carried
// through here for completeness).
type TmuxConfig struct {
	SessionName string `yaml:"session_name"`
	ConfigFile  string `yaml:"config_file"`
}

// DefaultsConfig holds fleet-wide fallback values for per-repo fields, so
// repos.yaml doesn't need to repeat default_branch/base on every entry.
type DefaultsConfig struct {
	DefaultBranch string `yaml:"default_branch"`
	BaseRoot      string `yaml:"base_root"`
}

// ReposConfig is the top-level shape of repos.yaml.
type ReposConfig struct {
	Tmux         TmuxConfig     `yaml:"tmux"`
	WorktreeRoot string         `yaml:"worktree_root"`
	Defaults     DefaultsConfig `yaml:"defaults"`
	Repos        []RepoConfig   `yaml:"repos"`
}

// applyDefaults fills any repo field left blank in repos.yaml with the
// top-level `defaults` values: default_branch falls back directly, and base
// falls back to <base_root>/<repo-name> when the repo omits `base`.
func (c *ReposConfig) applyDefaults() {
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.DefaultBranch == "" {
			r.DefaultBranch = c.Defaults.DefaultBranch
		}
		if r.Base == "" && c.Defaults.BaseRoot != "" {
			r.Base = path.Join(c.Defaults.BaseRoot, r.Name)
		}
	}
}

// loadReposConfig reads and parses repos.yaml from the given path.
func loadReposConfig(configPath string) (*ReposConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read repos config %s: %w", configPath, err)
	}
	var cfg ReposConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse repos config %s: %w", configPath, err)
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

// repoNames returns the names of all repos in the config, in file order.
func (c *ReposConfig) repoNames() []string {
	names := make([]string, 0, len(c.Repos))
	for _, r := range c.Repos {
		names = append(names, r.Name)
	}
	return names
}
