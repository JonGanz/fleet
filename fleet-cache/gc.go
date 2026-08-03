package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// referencedHashes walks each root directory recursively looking for
// package-lock.json files, hashes each one found, and returns the set of
// hashes still "referenced" by something on disk.
func referencedHashes(roots []string) (map[string]bool, error) {
	referenced := make(map[string]bool)
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				// Skip unreadable paths rather than aborting the whole scan.
				if os.IsNotExist(err) || os.IsPermission(err) {
					return nil
				}
				return err
			}
			if info.IsDir() && info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if !info.IsDir() && info.Name() == "package-lock.json" {
				hash, err := hashFile(path)
				if err != nil {
					return fmt.Errorf("hashing %s: %w", path, err)
				}
				referenced[hash] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return referenced, nil
}

// unreferencedEntries lists the cache entry hashes present under
// <node-cache dir> that are not in the referenced set.
func unreferencedEntries(nodeCache string, referenced map[string]bool) ([]string, error) {
	items, err := os.ReadDir(nodeCache)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var stale []string
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		if !referenced[item.Name()] {
			stale = append(stale, item.Name())
		}
	}
	return stale, nil
}

func runGC(roots []string, force bool) error {
	nc, err := nodeCacheDir()
	if err != nil {
		return err
	}

	referenced, err := referencedHashes(roots)
	if err != nil {
		return err
	}

	stale, err := unreferencedEntries(nc, referenced)
	if err != nil {
		return err
	}

	if len(stale) == 0 {
		fmt.Println("no stale cache entries found")
		return nil
	}

	dryRun := len(roots) == 0 || !force
	for _, hash := range stale {
		path := filepath.Join(nc, hash)
		if dryRun {
			fmt.Printf("would remove %s\n", path)
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
		fmt.Printf("removed %s\n", path)
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
