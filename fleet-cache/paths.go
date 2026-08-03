package main

import (
	"os"
	"path/filepath"
)

// cacheRoot returns the fleet cache root directory per the shared contract:
//
//	$FLEET_CACHE_DIR if set, else $XDG_CACHE_HOME/fleet if set, else ~/.cache/fleet
func cacheRoot() (string, error) {
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
func nodeCacheDir() (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "node-cache"), nil
}

// entryDir returns the cache entry directory for a given package-lock.json hash.
func entryDir(hash string) (string, error) {
	nc, err := nodeCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(nc, hash), nil
}
