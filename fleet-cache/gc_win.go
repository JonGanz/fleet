package main

import (
	"fmt"
	"path/filepath"
)

// runGCWindows is the runtime: windows counterpart of runGC: it reuses
// referencedHashes unchanged (it only walks WSL-visible roots and hashes
// lockfiles via Go, which is fine under the pragmatic scope decision), but
// scans/deletes against the Windows-native cache root's node-cache dir, and
// deletes stale entries via a native Windows Remove-Item rather than
// os.RemoveAll, since a stale hardlink-cache entry can contain many
// thousands of files.
func runGCWindows(roots []string, force bool, cacheRootFlag string) error {
	cacheRootWSL, err := resolveWindowsCacheRoot(cacheRootFlag)
	if err != nil {
		return fmt.Errorf("resolving windows cache root: %w", err)
	}
	ncWSL := filepath.Join(cacheRootWSL, "node-cache")

	referenced, err := referencedHashes(roots)
	if err != nil {
		return err
	}

	stale, err := unreferencedEntries(ncWSL, referenced)
	if err != nil {
		return err
	}

	if len(stale) == 0 {
		fmt.Println("no stale windows cache entries found")
		return nil
	}

	dryRun := len(roots) == 0 || !force
	for _, hash := range stale {
		wslPath := filepath.Join(ncWSL, hash)
		winPath, err := wslToWindowsPath(wslPath)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", wslPath, err)
		}
		if dryRun {
			fmt.Printf("would remove %s\n", winPath)
			continue
		}
		if err := runWindowsRemoveAll(winPath); err != nil {
			return fmt.Errorf("removing %s: %w", winPath, err)
		}
		fmt.Printf("removed %s\n", winPath)
	}

	if dryRun {
		if len(roots) == 0 {
			fmt.Println("no --roots given: nothing is considered referenced; pass --roots and --force to actually delete")
		} else {
			fmt.Println("dry run: pass --force to actually delete")
		}
	}

	return nil
}
