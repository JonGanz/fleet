package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// cacheKeyVersion prefixes the encoded key inputs. Bump it whenever the
// inputs or their encoding change, so old and new keys can never collide.
// (v1, implicitly, was the bare sha256 of package-lock.json.)
const cacheKeyVersion = "fleet-cache-key-v2"

// lockfileName is the npm lockfile a project's cache key is derived from.
const lockfileName = "package-lock.json"

// noNpmrc stands in for the .npmrc digest when the project has none. It
// can't collide with a real digest (always 64 hex chars), so "no .npmrc"
// and "empty .npmrc" get different keys.
const noNpmrc = "none"

// cacheKey returns <dir>/package-lock.json's path and the cache key of the
// project in dir: the name of its cache entry and the value of its
// .fleet-cache-hash marker. ensure, ensure-windows, the staging re-check
// and gc all derive keys through this one function, so they can't
// disagree.
//
// The key covers the lockfile and the sibling .npmrc (registry, auth,
// legacy-peer-deps, ... all change what `npm ci` installs). The .npmrc
// only enters as a sha256 digest, so no token ends up in the key.
func cacheKey(dir string) (lockPath, key string, err error) {
	lockPath = filepath.Join(dir, lockfileName)
	if _, err := os.Stat(lockPath); err != nil {
		return "", "", fmt.Errorf("no %s in %s: %w", lockfileName, dir, err)
	}
	lockHash, err := hashFile(lockPath)
	if err != nil {
		return "", "", fmt.Errorf("hashing %s: %w", lockPath, err)
	}
	npmrcHash, err := npmrcDigest(dir)
	if err != nil {
		return "", "", err
	}
	return lockPath, encodeCacheKey(lockHash, npmrcHash), nil
}

// encodeCacheKey hashes the versioned, line-based encoding of the key
// inputs. Both inputs are hex digests (or noNpmrc), so they can't contain a
// newline and the encoding is unambiguous.
func encodeCacheKey(lockHash, npmrcHash string) string {
	encoded := fmt.Sprintf("%s\nlock:%s\nnpmrc:%s\n", cacheKeyVersion, lockHash, npmrcHash)
	sum := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(sum[:])
}

// hashFile returns the hex-encoded sha256 digest of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// npmrcDigest returns the sha256 of <dir>/.npmrc, or noNpmrc if there is
// none (a dangling symlink counts as none, as it does for stageNpmrc).
// Anything other than a regular file (or a symlink to one) is an error,
// matching stageNpmrc, which refuses to install without it.
func npmrcDigest(dir string) (string, error) {
	path := filepath.Join(dir, npmrcName)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return noNpmrc, nil
	}
	if err != nil {
		return "", fmt.Errorf("checking %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	digest, err := hashFile(path)
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return digest, nil
}
