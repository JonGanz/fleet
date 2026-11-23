// Package config is the repos.yaml schema shared by fleet-task and
// fleet-run (see docs/CONTRACT.md): its types, loading, the `defaults`
// fallbacks and the validation every consumer relies on. Checks that only
// one tool needs (fleet-run's tmux window naming) live in that tool.
package config

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// RunConfig is one named run command for a repo (started by fleet-run), e.g.
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
	Name           string      `yaml:"name"`
	Origin         string      `yaml:"origin"`
	Base           string      `yaml:"base"`
	DefaultBranch  string      `yaml:"default_branch"`
	BranchTemplate string      `yaml:"branch_template"`
	Runtime        string      `yaml:"runtime"` // "linux" (default) or "windows"
	WindowsBase    string      `yaml:"windows_base"`
	Run            []RunConfig `yaml:"run"`
}

// IsWindows reports whether this repo is runtime: windows: its git
// operations and run commands go through Windows-native processes via
// powershell.exe rather than running directly in WSL/Linux.
func (r *RepoConfig) IsWindows() bool {
	return r.Runtime == "windows"
}

// TmuxConfig is the tmux section of repos.yaml (used by fleet-run).
type TmuxConfig struct {
	SessionName string `yaml:"session_name"`
	ConfigFile  string `yaml:"config_file"`
}

// DefaultsConfig holds fleet-wide fallback values for per-repo fields, so
// repos.yaml doesn't need to repeat default_branch/base on every entry.
type DefaultsConfig struct {
	DefaultBranch   string `yaml:"default_branch"`
	BranchTemplate  string `yaml:"branch_template"`
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
// falls back to <base_root>/<repo-name> when the repo omits `base`, for
// runtime: windows repos, windows_base falls back to
// <windows_base_root>/<repo-name> the same way, and branch_template falls
// back directly (further defaulting to "{ticket}" in fleet-task's
// branchName if still unset after this, i.e. a plain-ticket branch).
func (c *ReposConfig) applyDefaults() {
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.DefaultBranch == "" {
			r.DefaultBranch = c.Defaults.DefaultBranch
		}
		if r.BranchTemplate == "" {
			r.BranchTemplate = c.Defaults.BranchTemplate
		}
		if r.Runtime != "windows" && r.Base == "" && c.Defaults.BaseRoot != "" {
			r.Base = path.Join(c.Defaults.BaseRoot, r.Name)
		}
		if r.Runtime == "windows" && r.WindowsBase == "" && c.Defaults.WindowsBaseRoot != "" {
			r.WindowsBase = path.Join(c.Defaults.WindowsBaseRoot, r.Name)
		}
	}
}

// Load reads, parses and validates repos.yaml from configPath. Errors name
// the file.
func Load(configPath string) (*ReposConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read repos config %s: %w", configPath, err)
	}
	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse repos config %s: %w", configPath, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid repos config %s: %w", configPath, err)
	}
	return cfg, nil
}

// Parse parses and validates repos.yaml content already read into memory,
// applying `defaults`. Load is the same plus reading the file.
func Parse(data []byte) (*ReposConfig, error) {
	cfg, err := parse(data)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parse unmarshals data and applies defaults, without validating.
func parse(data []byte) (*ReposConfig, error) {
	var cfg ReposConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// FindRepo returns the RepoConfig with the given name, or nil if not found
// (also for a nil config).
func (c *ReposConfig) FindRepo(name string) *RepoConfig {
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

// RepoNames returns the names of all repos in the config, in file order.
func (c *ReposConfig) RepoNames() []string {
	names := make([]string, 0, len(c.Repos))
	for _, r := range c.Repos {
		names = append(names, r.Name)
	}
	return names
}

// validate rejects repo entries no fleet tool can use safely: a repo name
// becomes a directory component (<worktree_root>/<ticket>/<repo>, the
// patches dir, base_root/<repo>) and a lookup key, so it must be non-empty,
// unique and a single plain path component; runtime picks how git and run
// commands are executed, so a typo must not silently fall through to linux.
func (c *ReposConfig) validate() error {
	seen := make(map[string]bool, len(c.Repos))
	for i, r := range c.Repos {
		switch {
		case r.Name == "":
			return fmt.Errorf("repos[%d] has no name", i)
		case r.Name == "." || r.Name == ".." || strings.ContainsAny(r.Name, `/\`):
			return fmt.Errorf("repo name %q must be a plain name (no path separators, not . or ..)", r.Name)
		case seen[r.Name]:
			return fmt.Errorf("repo name %q is used more than once", r.Name)
		}
		seen[r.Name] = true
		switch r.Runtime {
		case "", "linux", "windows":
		default:
			return fmt.Errorf("repo %s: runtime %q must be linux or windows", r.Name, r.Runtime)
		}
	}
	return nil
}
