package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// hardlinkTree walks the tree rooted at src and recreates it rooted at dst:
//   - directories are created with os.MkdirAll
//   - regular files are hardlinked (os.Link) into place
//   - symlinks are recreated as symlinks (os.Readlink + os.Symlink), not
//     hardlinked, since some npm bin shims are symlinks and hardlinking a
//     symlink's target semantics would be wrong (and os.Link on a symlink
//     path links the target, not the link itself, on most platforms).
//
// dst must not already exist as a non-empty conflicting tree; callers that
// want a clean rebuild should os.RemoveAll(dst) first.
func hardlinkTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat source %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source %s is not a directory", src)
	}

	return filepath.Walk(src, func(path string, walkInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		mode := walkInfo.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", path, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// Remove any existing entry at target before recreating.
			if _, statErr := os.Lstat(target); statErr == nil {
				if err := os.Remove(target); err != nil {
					return err
				}
			}
			if err := os.Symlink(linkTarget, target); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", target, linkTarget, err)
			}
		case walkInfo.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if _, statErr := os.Lstat(target); statErr == nil {
				if err := os.Remove(target); err != nil {
					return err
				}
			}
			if err := os.Link(path, target); err != nil {
				return fmt.Errorf("link %s -> %s: %w", path, target, err)
			}
		default:
			// Skip other special file types (sockets, devices, etc.) — not
			// expected inside node_modules.
		}
		return nil
	})
}

// lockDownPermissions chmods every regular file under root read-only, while
// preserving each file's existing executable bits. Called once, right after
// `npm ci` populates a cache entry -- since permission bits belong to the
// inode, not the directory entry, this takes effect for every worktree the
// file is later hardlinked into, not just the cache copy. Directories are
// left writable so entries can still be added/removed/replaced (required for
// `ensure`'s own RemoveAll+rebuild and for `npm link`'s directory-entry-
// replacement mechanism to keep working); only in-place edits to existing
// file content are blocked, turning what would otherwise be silent
// cross-worktree corruption (every worktree shares this inode) into a loud
// permission error instead.
//
// Executable bits must survive this, or scripts invoked directly (e.g.
// node_modules/.bin symlink targets like env-cmd's) fail with "Permission
// denied" once hardlinked into a worktree -- a real regression caught via
// backend-api's env-cmd failing under `fleet-cache ensure`.
func lockDownPermissions(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			mode := os.FileMode(0o444) | (info.Mode() & 0o111)
			return os.Chmod(path, mode)
		}
		return nil
	})
}
