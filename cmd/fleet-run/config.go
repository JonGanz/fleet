package main

import (
	"fmt"
	"strings"

	"github.com/JonGanz/fleet/internal/config"
)

// loadReposConfig reads repos.yaml from configPath with the shared loader
// (internal/config) and then applies fleet-run's own checks
// (validateRunConfig).
func loadReposConfig(configPath string) (*config.ReposConfig, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if err := validateRunConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid repos config %s: %w", configPath, err)
	}
	return cfg, nil
}

// parseReposConfig is loadReposConfig for repos.yaml content already read
// into memory, so tests can exercise parsing without touching disk.
func parseReposConfig(data []byte) (*config.ReposConfig, error) {
	cfg, err := config.Parse(data)
	if err != nil {
		return nil, err
	}
	if err := validateRunConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validateRunConfig checks the invariants fleet-run relies on when turning
// repo/run-name pairs into picker labels ("repo:run") and tmux window names
// ("repo-run"): neither name may contain ":" (it would break "repo:run"
// parsing), and the derived window names must be unique across the whole
// config (e.g. repo "admin" + run "ui-dev" collides with repo "admin-ui" +
// run "dev", and duplicate run names within a repo collide with themselves).
// fleet-task never builds window names, so it does not run this check.
func validateRunConfig(c *config.ReposConfig) error {
	// tmux silently rewrites "." and ":" in a new session's name to "_", so
	// the session fleet-run creates would never be found again by name.
	if strings.ContainsAny(c.Tmux.SessionName, ".:") {
		return fmt.Errorf("tmux.session_name %q must not contain '.' or ':' (tmux would rename the session)", c.Tmux.SessionName)
	}
	seen := make(map[string]string) // window name -> "repo:run" that claimed it
	for _, r := range c.Repos {
		if strings.Contains(r.Name, ":") {
			return fmt.Errorf("repo name %q must not contain ':'", r.Name)
		}
		for _, run := range r.Run {
			if strings.Contains(run.Name, ":") {
				return fmt.Errorf("run name %q (repo %q) must not contain ':'", run.Name, r.Name)
			}
			wn := windowName(r.Name, run.Name)
			label := r.Name + ":" + run.Name
			if prev, dup := seen[wn]; dup {
				return fmt.Errorf("repos %q and %q both produce the tmux window name %q; rename a repo or run so every <repo>-<run-name> is unique", prev, label, wn)
			}
			seen[wn] = label
		}
	}
	return nil
}
