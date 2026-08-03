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
