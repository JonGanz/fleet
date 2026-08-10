package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// markerPath returns the path of the marker file `ensure` writes into a
// target node_modules recording which cache hash last populated it.
func markerPath(targetNodeModules string) string {
	return filepath.Join(targetNodeModules, ".fleet-cache-hash")
}

// alreadyLinked reports whether targetNodeModules's marker matches hash --
// i.e. a previous `ensure` call already populated it from this exact cache
// entry, so it's safe to skip re-linking and leave whatever the user has
// since added (e.g. an `npm link` symlink) untouched.
func alreadyLinked(targetNodeModules, hash string) bool {
	data, err := os.ReadFile(markerPath(targetNodeModules))
	return err == nil && strings.TrimSpace(string(data)) == hash
}

// writeMarker records hash as the cache entry that populated
// targetNodeModules, so a future `ensure` call can detect nothing's changed
// and skip the destructive rebuild.
func writeMarker(targetNodeModules, hash string) error {
	return os.WriteFile(markerPath(targetNodeModules), []byte(hash), 0o644)
}

// runEnsure implements `fleet-cache ensure <dir> [--force]`.
//
// This is Linux/WSL2-only. Windows-runtime repos (per repos.yaml's
// `runtime: windows` field, see docs/CONTRACT.md) use the separate
// `ensure-windows` command (ensure_win.go) instead, since hardlinks
// can't cross the WSL9P boundary.
func runEnsure(dir string, force bool) error {
	lockPath := filepath.Join(dir, "package-lock.json")
	if _, err := os.Stat(lockPath); err != nil {
		return fmt.Errorf("no package-lock.json in %s: %w", dir, err)
	}

	hash, err := hashFile(lockPath)
	if err != nil {
		return fmt.Errorf("hashing %s: %w", lockPath, err)
	}

	shortHash := hash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}

	targetNodeModules := filepath.Join(dir, "node_modules")

	// If this exact cache hash already populated the target, skip the
	// destructive rebuild entirely -- node_modules is otherwise treated as a
	// disposable derived artifact and blown away/relinked on every call,
	// which silently destroys anything the user has since added by hand
	// (most notably `npm link`'s package symlinks).
	if !force && alreadyLinked(targetNodeModules, hash) {
		fmt.Printf("node_modules already linked to cache %s, nothing to do\n", shortHash)
		return nil
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

	if _, err := os.Stat(targetNodeModules); err == nil {
		if err := os.RemoveAll(targetNodeModules); err != nil {
			return fmt.Errorf("removing existing %s: %w", targetNodeModules, err)
		}
	}

	if err := hardlinkTree(nodeModulesCache, targetNodeModules); err != nil {
		return fmt.Errorf("hardlinking node_modules into %s: %w", dir, err)
	}

	if err := writeMarker(targetNodeModules, hash); err != nil {
		fmt.Fprintf(os.Stderr, "warning: writing cache marker: %v\n", err)
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

	nodeModules := filepath.Join(entry, "node_modules")
	if _, err := os.Stat(nodeModules); err != nil {
		_ = os.RemoveAll(entry)
		return fmt.Errorf("npm ci completed but node_modules missing in %s: %w", entry, err)
	}

	if err := lockDownPermissions(nodeModules); err != nil {
		return fmt.Errorf("locking down cache entry permissions: %w", err)
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
