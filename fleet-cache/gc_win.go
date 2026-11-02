package main

import "fmt"

// runGCWindows is the runtime: windows counterpart of runGC: it shares
// gcStale (which only walks WSL-visible roots and hashes lockfiles via Go,
// fine under the pragmatic scope decision), but scans/deletes against the
// Windows-native cache root's node-cache dir, and deletes via a native
// Windows Remove-Item rather than os.RemoveAll, since a stale hardlink-cache
// entry can contain many thousands of files.
func runGCWindows(roots []string, force, asJSON bool, cacheRootFlag string) error {
	cacheRootWSL, err := resolveWindowsCacheRoot(cacheRootFlag)
	if err != nil {
		return fmt.Errorf("resolving windows cache root: %w", err)
	}
	return runGCCommon(nodeCacheDir(cacheRootWSL), "windows ", roots, force, asJSON,
		func(wslPath string) error {
			winPath, err := wslToWindowsPath(wslPath)
			if err != nil {
				return err
			}
			return runWindowsRemoveAll(winPath)
		},
		wslToWindowsPath)
}
