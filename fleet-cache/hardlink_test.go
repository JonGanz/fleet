package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHardlinkTree(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "node_modules")

	// Build a small fake node_modules tree:
	//   src/a.txt
	//   src/pkg/index.js
	//   src/.bin/tool -> ../pkg/index.js  (symlink, like an npm bin shim)
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "pkg", "index.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatalf("write index.js: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, ".bin"), 0o755); err != nil {
		t.Fatalf("mkdir .bin: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "pkg", "index.js"), filepath.Join(src, ".bin", "tool")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := hardlinkTree(src, dst); err != nil {
		t.Fatalf("hardlinkTree: %v", err)
	}

	// Regular files should be hardlinked: same inode as source.
	for _, rel := range []string{"a.txt", filepath.Join("pkg", "index.js")} {
		srcInfo, err := os.Stat(filepath.Join(src, rel))
		if err != nil {
			t.Fatalf("stat src %s: %v", rel, err)
		}
		dstInfo, err := os.Stat(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("stat dst %s: %v", rel, err)
		}
		if !os.SameFile(srcInfo, dstInfo) {
			t.Errorf("%s: expected dst to be a hardlink of src (same inode), but os.SameFile returned false", rel)
		}
	}

	// The symlink should be recreated as a symlink, not hardlinked/copied.
	dstLinkPath := filepath.Join(dst, ".bin", "tool")
	dstLstat, err := os.Lstat(dstLinkPath)
	if err != nil {
		t.Fatalf("lstat dst symlink: %v", err)
	}
	if dstLstat.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s: expected a symlink, got mode %v", dstLinkPath, dstLstat.Mode())
	}

	target, err := os.Readlink(dstLinkPath)
	if err != nil {
		t.Fatalf("readlink dst: %v", err)
	}
	wantTarget := filepath.Join("..", "pkg", "index.js")
	if target != wantTarget {
		t.Fatalf("symlink target = %q, want %q", target, wantTarget)
	}

	// Confirm the symlink itself is not sharing an inode with anything in
	// src (i.e. it wasn't hardlinked as if it were a regular file). We
	// verify this indirectly: the dst symlink's own inode (via Lstat) must
	// differ from the src symlink's own inode, since os.Symlink created a
	// brand new link rather than os.Link duplicating the directory entry.
	srcLstat, err := os.Lstat(filepath.Join(src, ".bin", "tool"))
	if err != nil {
		t.Fatalf("lstat src symlink: %v", err)
	}
	if os.SameFile(srcLstat, dstLstat) {
		t.Fatalf("expected symlink to be recreated independently, not hardlinked to the source symlink entry")
	}
}

func TestHardlinkTreeRequiresDirSource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := hardlinkTree(file, filepath.Join(dir, "dst")); err == nil {
		t.Fatal("expected error when source is not a directory")
	}
}

func TestLockDownPermissions(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "index.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatalf("write index.js: %v", err)
	}
	if err := os.Symlink(filepath.Join("pkg", "index.js"), filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := lockDownPermissions(root); err != nil {
		t.Fatalf("lockDownPermissions: %v", err)
	}

	fileInfo, err := os.Stat(filepath.Join(root, "pkg", "index.js"))
	if err != nil {
		t.Fatalf("stat index.js: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o444 {
		t.Errorf("index.js perm = %o, want 0444", perm)
	}

	dirInfo, err := os.Stat(filepath.Join(root, "pkg"))
	if err != nil {
		t.Fatalf("stat pkg dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o755 {
		t.Errorf("pkg dir perm = %o, want unchanged 0755 (directories must stay writable)", perm)
	}

	linkInfo, err := os.Lstat(filepath.Join(root, "link"))
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected link to remain a symlink, got mode %v", linkInfo.Mode())
	}

	// Removing the now-read-only file must still succeed: unlink only needs
	// write permission on the containing directory, not the file itself --
	// this is what keeps ensure's RemoveAll+rebuild and npm link's
	// directory-entry replacement working despite the lockdown.
	if err := os.Remove(filepath.Join(root, "pkg", "index.js")); err != nil {
		t.Errorf("removing read-only file should succeed (dir stays writable): %v", err)
	}
}
