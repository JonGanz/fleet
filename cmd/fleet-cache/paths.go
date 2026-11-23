package main

import (
	"path/filepath"
)

// nodeCacheDir returns <cache root>/node-cache.
func nodeCacheDir(root string) string {
	return filepath.Join(root, "node-cache")
}
