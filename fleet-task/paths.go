package main

import (
	"os"
	"path/filepath"
)

// configDir returns the fleet config directory per the shared contract:
//
//	$FLEET_CONFIG_DIR if set, else $XDG_CONFIG_HOME/fleet if set, else ~/.config/fleet
func configDir() (string, error) {
	if v := os.Getenv("FLEET_CONFIG_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "fleet"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fleet"), nil
}

// reposFile returns the path to repos.yaml per the shared contract:
//
//	$FLEET_REPOS_FILE if set, else <config dir>/repos.yaml
func reposFile() (string, error) {
	if v := os.Getenv("FLEET_REPOS_FILE"); v != "" {
		return v, nil
	}
	cd, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "repos.yaml"), nil
}

// patchesDir returns <config dir>/patches/<repo>.
func patchesDir(repo string) (string, error) {
	cd, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "patches", repo), nil
}

// hooksDir returns <config dir>/hooks/<phase>.
func hooksDir(phase string) (string, error) {
	cd, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "hooks", phase), nil
}

// stateDir returns the fleet state directory per the shared contract:
//
//	$FLEET_STATE_DIR if set, else $XDG_STATE_HOME/fleet if set, else ~/.local/state/fleet
func stateDir() (string, error) {
	if v := os.Getenv("FLEET_STATE_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "fleet"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "fleet"), nil
}

// tasksDir returns <state dir>/tasks.
func tasksDir() (string, error) {
	sd, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(sd, "tasks"), nil
}

// taskFile returns <state dir>/tasks/<ticket>.json.
func taskFile(ticket string) (string, error) {
	td, err := tasksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(td, ticket+".json"), nil
}

// defaultWorktreeRoot returns <state dir>/worktrees, used when repos.yaml
// does not set worktree_root.
func defaultWorktreeRoot() (string, error) {
	sd, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(sd, "worktrees"), nil
}

// worktreeRoot returns the effective worktree root: repos.yaml's
// worktree_root if set, else the default state-dir location.
func worktreeRoot(cfg *ReposConfig) (string, error) {
	if cfg != nil && cfg.WorktreeRoot != "" {
		return expandHome(cfg.WorktreeRoot), nil
	}
	return defaultWorktreeRoot()
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(p string) string {
	if p == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return p
	}
	if len(p) >= 2 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
