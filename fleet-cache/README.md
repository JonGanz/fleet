# fleet-cache

Part of the `fleet` suite (see `../docs/CONTRACT.md`). A
`node_modules` hardlink cache keyed by the sha256 hash of a repo's
`package-lock.json`, so repeated `npm ci` across many git worktrees of the
same repo (same lockfile) don't redundantly reinstall — instead a cached
`node_modules` tree is hardlinked into place (a hardlink is another directory
entry for the same file on disk, so extra copies take no space).

`ensure` and `gc` are for Linux/WSL2 (`runtime: linux`) repos. Windows-runtime
repos (per `repos.yaml`'s `runtime: windows`) use the separate `ensure-windows`/
`gc-windows` commands below instead, since hardlinks can't cross the WSL 9P
boundary (the file-sharing protocol WSL2 uses to reach Windows drives) — those populate a separate, Windows-native cache via native
PowerShell/npm processes rather than WSL reaching into `/mnt/...`.

## Cache layout

```
<cache root>/
├── hardlink.ps1        # embedded Windows hardlink script (windows cache root only)
└── node-cache/
    ├── <sha256 of package-lock.json>/
    │   ├── package-lock.json   # copy of the lockfile that produced this entry
    │   ├── package.json        # copy of the manifest (`npm ci` needs both)
    │   └── node_modules/       # populated once via `npm ci`, reused thereafter
    └── <sha256>.tmp-<pid>/     # staging dir while an entry is being built
```

`<cache root>` resolves to, in order: `--cache-root`, else
`$FLEET_CACHE_DIR`, else `$XDG_CACHE_HOME/fleet`, else `~/.cache/fleet`.

**Entries are built in a staging dir and renamed into place.** An entry
directory therefore only exists once it is fully built: a killed `npm ci`
leaves only a `<hash>.tmp-<pid>` staging dir (never a reusable partial
entry), and staging dirs are removed on any failure. Concurrent `ensure`s of
the same hash don't corrupt each other: the loser's rename finds the
winner's entry already present, discards its own staging dir and uses the
winner's. `gc` reports leftover staging dirs older than a day (see below).

**Conventions common to every command:**

- Flags may come before or after the positional `<dir>` (`ensure <dir>
  --force` and `ensure --force <dir>` are equivalent).
- Progress/status messages, and npm's own output, go to **stderr**; stdout
  carries only results (gc's report, `--json`), so output doesn't interleave
  with `fleet-task new`'s.
- Usage errors print the usage text and exit 1 (like fleet-task/fleet-run);
  `--help`/`-h` print usage to stdout and exit 0.
- `--cache-root` is accepted by all four commands and overrides the cache
  root resolution above (for the Windows commands see below).

## Commands

### `fleet-cache ensure <dir> [--force] [--cache-root <dir>]`

`<dir>` is a worktree/repo directory containing a `package-lock.json`.

1. Hashes `<dir>/package-lock.json` (sha256). Errors out (non-zero exit,
   message on stderr) if the file is missing.
2. **Skip-if-unchanged**: if `<dir>/node_modules/.fleet-cache-hash` already
   records this exact hash (written by a prior successful `ensure`), prints
   `node_modules already linked to cache <hash8chars>, nothing to do` (stderr) and
   stops here, touching nothing else. This is what makes it safe to `npm
   link` a package inside a worktree's `node_modules` and rerun `ensure`
   later (e.g. from another `fleet-task` command) without losing the link —
   without this check, `node_modules` is otherwise treated as a disposable
   derived artifact and unconditionally rebuilt from the cache on every
   call, silently discarding anything added by hand. Pass `--force` to
   rebuild anyway (`--force` means *rebuild*: `<dir>/node_modules` is
   discarded and re-linked from the cache). If the lockfile hash *has*
   changed, `ensure` always rebuilds regardless of `--force` — a real dependency change is expected
   to replace whatever was there, `npm link`ed packages included.
3. Checks the cache root and `<dir>` are on the same filesystem (compares
   device ids) and errors clearly *before* any `npm ci` if not, since
   hardlinks can't cross filesystems. (An `EXDEV` from an individual
   `link` call later on also produces a clear message.)
4. If no cache entry exists for that hash yet: builds one in
   `<cache root>/node-cache/<hash>.tmp-<pid>/` — copies `package-lock.json`
   *and* `package.json` in and runs `npm ci` there (npm's output goes to
   stderr so you see its progress) — then atomically renames it to
   `<cache root>/node-cache/<hash>/`.
   Every regular file in the resulting `node_modules` is then chmod'd
   read-only (`0444`) — since permission bits belong to the shared inode,
   not the directory entry, this takes effect for every worktree the file
   is later hardlinked into, not just this cache copy. Directories stay
   writable, so entries can still be added/removed/replaced (this is how
   `ensure`'s own rebuild and `npm link`'s symlink-swap both keep working);
   only in-place edits to existing file *content* are blocked, turning what
   would otherwise be silent corruption shared across every worktree using
   that lockfile hash into a loud permission error instead.
5. If `<dir>/node_modules` already exists (and step 2 didn't already skip),
   removes it (it's a disposable derived artifact).
6. Hardlinks the cache entry's `node_modules` tree into `<dir>/node_modules`:
   regular files become hardlinks (`os.Link`); symlinks (e.g. npm bin shims)
   are recreated as symlinks rather than hardlinked. Writes the
   `.fleet-cache-hash` marker used by step 2's skip check.
7. Prints `linked node_modules from cache <hash8chars>` (to stderr) on success.

Intended to be called by `fleet-task new` after creating each repo's
worktree, per the CLI contract — a cache failure for one repo should not
abort the whole `fleet-task new` run.

### `fleet-cache gc [--roots <dir>[,<dir>...]] [--force] [--json] [--cache-root <dir>]`

Garbage-collects cache entries that are no longer referenced by any known
`package-lock.json`.

- `--roots`: one or more directories (flag may be repeated, and/or each
  value may be comma-separated) to scan recursively for `package-lock.json`
  files. Every lockfile found is hashed; any cache entry whose hash isn't in
  that "still referenced" set is considered stale. (`node_modules`
  directories are skipped while scanning, so lockfiles vendored inside
  dependencies don't count; symlinked directories, including the WSL-side
  symlinks fleet-task makes for windows-runtime worktrees, are followed.)
  **Every root must exist and be a readable directory**, and any unreadable
  path under it is an error: gc exits non-zero and deletes nothing, so a
  typo'd root can't make the whole cache look unreferenced.
- `--force`: *really delete* the stale entries. Without it, `gc` only prints
  what it *would* remove.
- `--json`: print the report on stdout as JSON instead of text, in dry-run
  and force mode alike: `{"node_cache": ..., "dry_run": bool, "stale":
  [{"name", "path", "kind": "entry"|"staging", "removed": bool}]}`.
- Also flags abandoned `<hash>.tmp-<pid>` staging dirs older than a day
  (`kind: "staging"`; younger ones may belong to a running `ensure` and are
  never touched), removed under the same `--roots` + `--force` rule.
- `--cache-root`: use this cache root instead of the default resolution.
- If `--roots` is omitted entirely, nothing is treated as referenced, and
  `gc` always runs in report-only mode regardless of `--force` — avoids
  accidentally wiping the whole cache from a bare `fleet-cache gc --force`.

```
fleet-cache gc --roots ~/.local/state/fleet/worktrees --force
```

### `fleet-cache ensure-windows <wsl-dir> [--force] [--cache-root <wsl-path>]`

Windows-native counterpart of `ensure`, for `runtime: windows` repos.
`<wsl-dir>` is the worktree's WSL-visible path (e.g. under `/mnt/c/...`,
since fleet-task keeps the real git worktree on an NTFS volume for these
repos and only symlinks it into the normal WSL worktree location).

Cache root resolution: `--cache-root` if given (repos.yaml's
`windows_cache_root`, already expanded to a WSL-visible path), else
`%LOCALAPPDATA%\fleet` auto-detected via `powershell.exe` (output forced to
UTF-8 so non-ASCII usernames survive), mirroring the Linux default of
`~/.cache/fleet`. Entries live at `<cache root>\node-cache\<sha256>\`.

Same skip-marker and `--force` semantics as `ensure`: if
`<wsl-dir>/node_modules/.fleet-cache-hash` already matches the lockfile
hash, nothing is touched (so `npm link`s survive reruns); `--force` rebuilds
(discarding manual changes). Cache entries are built via the same
staging-dir-and-rename scheme as `ensure`, with `npm ci` run natively in the
staging dir.

Only genuinely bulk filesystem work — `npm ci`, removing an old
`node_modules` tree, and hardlink-populating the target `node_modules` —
runs as native Windows processes (PowerShell/npm). Single-file operations
(hashing the lockfile, copying `package.json`/`package-lock.json` into the
cache entry, the skip marker) stay as ordinary Go file I/O against the `/mnt/...` path, since
those are negligible one-off costs, not the many-small-file operations that
are slow across the WSL 9P boundary.

The hardlink population itself runs via an embedded PowerShell script
(`hardlink_windows.ps1`, loaded with `go:embed`) that mirrors `hardlink.go`'s
directory/hardlink/symlink handling as native `New-Item` calls (walking one
level at a time so Windows PowerShell 5.1 never descends into reparse
points). It is written to `<cache root>/hardlink.ps1`, and only rewritten
when its content differs. Both the
cache entry and the target worktree must be on the same NTFS drive —
`ensure-windows` checks this up front and errors clearly if not, since
`New-Item -ItemType HardLink` fails across volumes.

PowerShell failures propagate: the generated commands abort if `cd` fails
and exit non-zero if a cmdlet such as `Remove-Item` fails (e.g. a locked
file), rather than reporting success.

### `fleet-cache gc-windows [--roots <dir>[,<dir>...]] [--force] [--json] [--cache-root <wsl-path>]`

Windows-native counterpart of `gc` — shares its core (same `--roots`
validation, `--force`/`--json` rules, staging-dir cleanup; no `--roots` ⇒
always report-only), scanning/deleting against the
Windows-native cache root's `node-cache` dir via `Remove-Item -Recurse -Force`
instead of `os.RemoveAll`.

## Development

```
go build ./... && go vet ./... && go test ./... && gofmt -l .
```

Tests avoid running real `npm ci` (no network calls in `go test`); the
`ensure` cache-hit and hardlink-walk logic is exercised against
pre-populated fake directories instead.
