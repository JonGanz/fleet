package main

import (
	"os"
	"path/filepath"
)

// cacheRoot returns the fleet cache root directory per the shared contract:
//
//	--cache-root if given, else $FLEET_CACHE_DIR, else $XDG_CACHE_HOME/fleet,
//	else ~/.cache/fleet
func cacheRoot(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if v := os.Getenv("FLEET_CACHE_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "fleet"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "fleet"), nil
}

// nodeCacheDir returns <cache root>/node-cache.
func nodeCacheDir(root string) string {
	return filepath.Join(root, "node-cache")
}
