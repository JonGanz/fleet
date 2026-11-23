// Package xdg resolves fleet's config, state and cache directories per
// docs/CONTRACT.md: a FLEET_* override if set, else the XDG base directory
// variable, else the XDG default under the home directory.
//
// A FLEET_* override (and fleet-cache's --cache-root) may be relative; it is
// made absolute against the working directory, which the fleet binaries
// never change, so every call resolves it the same way. A relative
// XDG_*_HOME value is invalid per the XDG Base Directory spec: it is
// ignored with a single warning per process and the default is used.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/JonGanz/fleet/internal/cli"
)

// Environment variables that override the XDG locations.
const (
	ConfigDirEnv = "FLEET_CONFIG_DIR"
	ReposFileEnv = "FLEET_REPOS_FILE"
	StateDirEnv  = "FLEET_STATE_DIR"
	CacheDirEnv  = "FLEET_CACHE_DIR"
)

// warned records which "VAR=value" pairs have already been warned about, so
// a relative XDG_*_HOME produces one warning line however often it is read.
var warned sync.Map

// absolute makes an override value absolute against the working directory.
// what names the source in the error.
func absolute(what, v string) (string, error) {
	abs, err := filepath.Abs(v)
	if err != nil {
		return "", fmt.Errorf("resolve %s %q to an absolute path: %w", what, v, err)
	}
	return abs, nil
}

// CacheDir returns the fleet cache root:
//
//	flagVal (fleet-cache's --cache-root) if non-empty, else
//	$FLEET_CACHE_DIR if set, else $XDG_CACHE_HOME/fleet if set and
//	absolute, else ~/.cache/fleet
func CacheDir(flagVal string) (string, error) {
	if flagVal != "" {
		return absolute("--cache-root", flagVal)
	}
	return resolve(CacheDirEnv, "XDG_CACHE_HOME", ".cache")
}

// ConfigDir returns the fleet config directory:
//
//	$FLEET_CONFIG_DIR if set, else $XDG_CONFIG_HOME/fleet if set and
//	absolute, else ~/.config/fleet
func ConfigDir() (string, error) {
	return resolve(ConfigDirEnv, "XDG_CONFIG_HOME", ".config")
}

// DefaultWorktreeRoot returns <state dir>/worktrees, the worktree root used
// when repos.yaml does not set worktree_root.
func DefaultWorktreeRoot() (string, error) {
	sd, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(sd, "worktrees"), nil
}

// ExpandHome expands a leading "~" or "~/" to the user's home directory,
// leaving p unchanged if there is none or the home directory is unknown.
func ExpandHome(p string) string {
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

// HooksDir returns <config dir>/hooks/<phase>.
func HooksDir(phase string) (string, error) {
	cd, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "hooks", phase), nil
}

// PatchesDir returns <config dir>/patches/<repo>.
func PatchesDir(repo string) (string, error) {
	cd, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "patches", repo), nil
}

// ReposFile returns the path of repos.yaml:
//
//	$FLEET_REPOS_FILE if set, else <config dir>/repos.yaml
func ReposFile() (string, error) {
	if v := os.Getenv(ReposFileEnv); v != "" {
		return absolute(ReposFileEnv, v)
	}
	cd, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "repos.yaml"), nil
}

// resolve implements the lookup order shared by the config, state and cache
// directories: the FLEET_* override, else <$xdgEnv>/fleet when that is an
// absolute path, else <home>/<homeSub>/fleet.
func resolve(fleetEnv, xdgEnv, homeSub string) (string, error) {
	if v := os.Getenv(fleetEnv); v != "" {
		return absolute(fleetEnv, v)
	}
	if v := os.Getenv(xdgEnv); v != "" {
		if filepath.IsAbs(v) {
			return filepath.Join(v, "fleet"), nil
		}
		warnRelative(xdgEnv, v, homeSub)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, homeSub, "fleet"), nil
}

// StateDir returns the fleet state directory:
//
//	$FLEET_STATE_DIR if set, else $XDG_STATE_HOME/fleet if set and
//	absolute, else ~/.local/state/fleet
func StateDir() (string, error) {
	return resolve(StateDirEnv, "XDG_STATE_HOME", filepath.Join(".local", "state"))
}

// TasksDir returns <state dir>/tasks, where the per-ticket state files live.
func TasksDir() (string, error) {
	sd, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(sd, "tasks"), nil
}

// warnRelative prints, once per variable and value, that a relative
// XDG_*_HOME is being ignored.
func warnRelative(env, value, homeSub string) {
	if _, dup := warned.LoadOrStore(env+"="+value, true); dup {
		return
	}
	cli.Warnf("ignoring %s=%q: the XDG spec requires an absolute path; using ~/%s instead", env, value, filepath.ToSlash(homeSub))
}
