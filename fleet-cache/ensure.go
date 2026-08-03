package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// runEnsure implements `fleet-cache ensure <dir>`.
//
// NOTE: this is Linux/WSL2-only. Windows-runtime repos (per repos.yaml's
// `runtime: windows` field, see docs/CONTRACT.md) need a separate cache path
// and mechanism since hardlinks can't cross the WSL9P boundary; that's out
// of scope for this first version.
func runEnsure(dir string) error {
	lockPath := filepath.Join(dir, "package-lock.json")
	if _, err := os.Stat(lockPath); err != nil {
		return fmt.Errorf("no package-lock.json in %s: %w", dir, err)
	}

	hash, err := hashFile(lockPath)
	if err != nil {
		return fmt.Errorf("hashing %s: %w", lockPath, err)
	}

	entry, err := entryDir(hash)
	if err != nil {
		return fmt.Errorf("resolving cache dir: %w", err)
	}

	nodeModulesCache := filepath.Join(entry, "node_modules")
	if _, err := os.Stat(nodeModulesCache); os.IsNotExist(err) {
		if err := populateCacheEntry(entry, lockPath); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("checking cache entry %s: %w", entry, err)
	}

	targetNodeModules := filepath.Join(dir, "node_modules")
	if _, err := os.Stat(targetNodeModules); err == nil {
		if err := os.RemoveAll(targetNodeModules); err != nil {
			return fmt.Errorf("removing existing %s: %w", targetNodeModules, err)
		}
	}

	if err := hardlinkTree(nodeModulesCache, targetNodeModules); err != nil {
		return fmt.Errorf("hardlinking node_modules into %s: %w", dir, err)
	}

	shortHash := hash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	fmt.Printf("linked node_modules from cache %s\n", shortHash)
	return nil
}

// populateCacheEntry creates the cache entry directory, copies the
// package-lock.json and package.json in, and runs `npm ci` there so
// node_modules gets populated in the cache dir.
//
// `npm ci` requires both package.json and package-lock.json to be present
// in its working directory (it validates the lockfile against the
// manifest) -- copying the lockfile alone fails with an ENOENT reading
// package.json.
func populateCacheEntry(entry, lockPath string) error {
	if err := os.MkdirAll(entry, 0o755); err != nil {
		return fmt.Errorf("creating cache entry dir %s: %w", entry, err)
	}

	if err := copyFile(lockPath, filepath.Join(entry, "package-lock.json")); err != nil {
		return fmt.Errorf("copying package-lock.json into cache: %w", err)
	}

	pkgPath := filepath.Join(filepath.Dir(lockPath), "package.json")
	if err := copyFile(pkgPath, filepath.Join(entry, "package.json")); err != nil {
		return fmt.Errorf("copying package.json into cache: %w", err)
	}

	cmd := exec.Command("npm", "ci")
	cmd.Dir = entry
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Best-effort cleanup so a failed install doesn't leave a bad cache
		// entry that future `ensure` calls treat as complete.
		_ = os.RemoveAll(entry)
		return fmt.Errorf("npm ci failed in %s: %w", entry, err)
	}

	if _, err := os.Stat(filepath.Join(entry, "node_modules")); err != nil {
		_ = os.RemoveAll(entry)
		return fmt.Errorf("npm ci completed but node_modules missing in %s: %w", entry, err)
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
