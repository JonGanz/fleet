# fleet-cache

Part of the `fleet` suite (see `../docs/CONTRACT.md`). A
`node_modules` hardlink cache keyed by the sha256 hash of a repo's
`package-lock.json`, so repeated `npm ci` across many git worktrees of the
same repo (same lockfile) don't redundantly reinstall — instead a cached
`node_modules` tree is hardlinked into place.

`ensure`/`gc` above are Linux/WSL2 only. Windows-runtime repos (per
`repos.yaml`'s `runtime: windows`) use the separate `ensure-windows`/
`gc-windows` commands below instead, since hardlinks can't cross the WSL9P
boundary — those populate a separate, Windows-native cache via native
PowerShell/npm processes rather than WSL reaching into `/mnt/...`.

## Cache layout

```
<cache root>/node-cache/<sha256 of package-lock.json>/
├── package-lock.json   # copy of the lockfile that produced this entry
└── node_modules/       # populated once via `npm ci`, reused thereafter
```

`<cache root>` resolves to, in order: `$FLEET_CACHE_DIR`, else
`$XDG_CACHE_HOME/fleet`, else `~/.cache/fleet`.

## Commands

### `fleet-cache ensure <dir>`

`<dir>` is a worktree/repo directory containing a `package-lock.json`.

1. Hashes `<dir>/package-lock.json` (sha256). Errors out (non-zero exit,
   message on stderr) if the file is missing.
2. If no cache entry exists for that hash yet: creates
   `<cache root>/node-cache/<hash>/`, copies the lockfile in, and runs
   `npm ci` there (with stdout/stderr inherited so you see npm's progress).
3. If `<dir>/node_modules` already exists, removes it (it's a disposable
   derived artifact).
4. Hardlinks the cache entry's `node_modules` tree into `<dir>/node_modules`:
   regular files become hardlinks (`os.Link`); symlinks (e.g. npm bin shims)
   are recreated as symlinks rather than hardlinked.
5. Prints `linked node_modules from cache <hash8chars>` on success.

Intended to be called by `fleet-task new` after creating each repo's
worktree, per the CLI contract — a cache failure for one repo should not
abort the whole `fleet-task new` run.

### `fleet-cache gc [--roots <dir>[,<dir>...]] [--force]`

Garbage-collects cache entries that are no longer referenced by any known
`package-lock.json`.

- `--roots`: one or more directories (flag may be repeated, and/or each
  value may be comma-separated) to scan recursively for `package-lock.json`
  files. Every lockfile found is hashed; any cache entry whose hash isn't in
  that "still referenced" set is considered stale. (`node_modules`
  directories are skipped while scanning, so lockfiles vendored inside
  dependencies don't count.)
- `--force`: actually delete stale entries. Without it, `gc` only prints
  what it *would* remove.
- If `--roots` is omitted entirely, nothing is treated as referenced, and
  `gc` always runs in report-only mode regardless of `--force` — avoids
  accidentally wiping the whole cache from a bare `fleet-cache gc --force`.

```
fleet-cache gc --roots ~/.local/state/fleet/worktrees --force
```

### `fleet-cache ensure-windows <wsl-dir> [--cache-root <wsl-path>]`

Windows-native counterpart of `ensure`, for `runtime: windows` repos.
`<wsl-dir>` is the worktree's WSL-visible path (e.g. under `/mnt/c/...`,
since fleet-task keeps the real git worktree on an NTFS volume for these
repos and only symlinks it into the normal WSL worktree location).

Cache root resolution: `--cache-root` if given (repos.yaml's
`windows_cache_root`, already expanded to a WSL-visible path), else
`%LOCALAPPDATA%\fleet` auto-detected via `cmd.exe`, mirroring the Linux
default of `~/.cache/fleet`. Entries live at
`<cache root>\node-cache\<sha256>\`.

Only genuinely bulk filesystem work — `npm ci`, removing an old
`node_modules` tree, and hardlink-populating the target `node_modules` —
runs as native Windows processes (PowerShell/npm). Single-file operations
(hashing the lockfile, copying `package.json`/`package-lock.json` into the
cache entry) stay as ordinary Go file I/O against the `/mnt/...` path, since
those are negligible one-off costs, not the many-small-file operations that
are slow across the WSL9P boundary.

The hardlink population itself runs via an embedded PowerShell script
(`hardlink_windows.ps1`, loaded with `go:embed`) that mirrors `hardlink.go`'s
directory/hardlink/symlink handling as native `New-Item` calls. Both the
cache entry and the target worktree must be on the same NTFS drive —
`ensure-windows` checks this up front and errors clearly if not, since
`New-Item -ItemType HardLink` fails across volumes.

### `fleet-cache gc-windows [--roots <dir>[,<dir>...]] [--force] [--cache-root <wsl-path>]`

Windows-native counterpart of `gc` — same `--roots`/`--force` safety rule
(no `--roots` ⇒ always report-only), scanning/deleting against the
Windows-native cache root's `node-cache` dir via `Remove-Item -Recurse -Force`
instead of `os.RemoveAll`.

## Development

```
go build ./...
go test ./...
```

Tests avoid running real `npm ci` (no network calls in `go test`); the
`ensure` cache-hit and hardlink-walk logic is exercised against
pre-populated fake directories instead.
