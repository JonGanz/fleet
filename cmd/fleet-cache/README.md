# fleet-cache

Part of the `fleet` suite (see `../../docs/CONTRACT.md`; this is
`cmd/fleet-cache/` of the fleet Go module). A
`node_modules` hardlink cache keyed by a repo's `package-lock.json` plus
its `.npmrc` (see [Cache key](#cache-key)), so repeated `npm ci` across many
git worktrees of the same repo (same lockfile and npm config) don't
redundantly reinstall — instead a cached
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
    ├── <cache key>/            # 64 hex chars, see "Cache key" below
    │   ├── package-lock.json   # copy of the lockfile that produced this entry
    │   ├── package.json        # copy of the manifest (`npm ci` needs both)
    │   │                         # (.npmrc is staged too, but only while npm ci runs)
    │   ├── node_modules/       # populated once via `npm ci`, reused thereafter
    │   └── .fleet-cache-complete # written last; an entry without it is rebuilt
    └── <key>.tmp-<pid>/        # staging dir while an entry is being built
```

`<cache root>` resolves to, in order: `--cache-root`, else
`$FLEET_CACHE_DIR`, else `$XDG_CACHE_HOME/fleet`, else `~/.cache/fleet`
(shared code, `internal/xdg`). A relative `--cache-root` or
`$FLEET_CACHE_DIR` is taken relative to the current directory; a relative
`$XDG_CACHE_HOME` is ignored with a warning, per the XDG spec.

**Entries are built in a staging dir and renamed into place.** An entry
directory therefore only exists once it is fully built: a killed `npm ci`
leaves only a `<key>.tmp-<pid>` staging dir (never a reusable partial
entry), and staging dirs are removed on any failure. Concurrent `ensure`s of
the same key don't corrupt each other: the loser's rename finds the
winner's entry already present, discards its own staging dir and uses the
winner's. A `.fleet-cache-complete` marker is written into the staging dir
just before the rename; an entry dir without it (e.g. a legacy entry built
before staging existed, possibly with a partial `node_modules`) is deleted
and rebuilt. `gc` reports leftover staging dirs older than a day (see below).

**Conventions common to every command:**

- Flags may come before or after the positional `<dir>` (`ensure <dir>
  --force` and `ensure --force <dir>` are equivalent).
- Progress/status messages, and npm's own output, go to **stderr**; stdout
  carries only results (gc's report, list, `--json`, version), so output doesn't interleave
  with `fleet-task new`'s.
- Exit codes (as in fleet-task/fleet-run): `0` ok, `1` operation failed
  (including an interrupted `ensure`), `2` usage error (unknown command,
  bad flag, wrong number of arguments; the usage text goes to stderr).
  Errors and warnings are printed as `fleet-cache: error: <command>: <msg>`
  and `fleet-cache: warning: <msg>`, like the other two tools.
  `--help`/`-h`/`help` print usage to stdout and exit 0.
- `fleet-cache version` / `fleet-cache --version` print
  `fleet-cache <version>`: the value set at build time with
  `-ldflags "-X main.version=<version>"`, else the module version or VCS
  revision Go recorded in the binary (`-dirty` if built from a modified
  tree), else `dev`.
- `--cache-root` is accepted by every command except `version` and
  overrides the cache root resolution above (for the Windows commands see
  below).

## Cache key

An entry's name (and the value of the `.fleet-cache-hash` marker `ensure`
writes) is

```
sha256("fleet-cache-key-v2\nlock:<L>\nnpmrc:<N>\n")
```

as 64 lowercase hex chars, where `<L>` is the sha256 (hex) of
`<dir>/package-lock.json` and `<N>` is the sha256 (hex) of `<dir>/.npmrc`,
or the literal `none` if there is no `.npmrc` (a dangling symlink counts as
none; a `.npmrc` that isn't a regular file is an error). So two worktrees
with the same lockfile but a different `.npmrc` (registry, auth,
`legacy-peer-deps`, ...) get separate entries. A missing `.npmrc` and an
empty one are deliberately *different* keys: it costs at most one extra
entry and keeps the encoding free of special cases. Only the project-level
`<dir>/.npmrc` is keyed, not the user's `~/.npmrc` or npm environment
variables. `ensure`, `ensure-windows` and `gc`/`gc-windows` all compute the
key through one function, so they can't disagree.

The `.npmrc` (and any registry token in it) enters the key only through
its sha256, so no secret is stored in the cache's names or markers. A
consequence: rotating a token in `.npmrc` changes the key, so the next
`ensure` builds a new entry and the old one becomes unreferenced (`gc`
reports it stale).

Entries and markers from fleet-cache versions that keyed by the bare
lockfile hash are not migrated: the first `ensure` after upgrading relinks
each worktree once (its old marker no longer matches), and the old entries
are unreferenced, so `gc` reports/removes them like any stale entry.

## Commands

### `fleet-cache ensure <dir> [--force] [--cache-root <dir>]`

`<dir>` is a worktree/repo directory containing a `package-lock.json`.

1. Computes the [cache key](#cache-key) from `<dir>/package-lock.json` and
   `<dir>/.npmrc`. Errors out (non-zero exit, message on stderr) if the
   lockfile is missing.
2. **Skip-if-unchanged**: if `<dir>/node_modules/.fleet-cache-hash` already
   records this exact key (written by a prior successful `ensure`), prints
   `node_modules already linked to cache <key8chars>, nothing to do` (stderr) and
   stops here, touching nothing else. This is what makes it safe to `npm
   link` a package inside a worktree's `node_modules` and rerun `ensure`
   later (e.g. from another `fleet-task` command) without losing the link —
   without this check, `node_modules` is otherwise treated as a disposable
   derived artifact and unconditionally rebuilt from the cache on every
   call, silently discarding anything added by hand. Pass `--force` to
   rebuild anyway (`--force` means *rebuild*: `<dir>/node_modules` is
   discarded and re-linked from the cache). If the key *has* changed
   (lockfile or `.npmrc` edited), `ensure` always rebuilds regardless of
   `--force` — a real dependency/config change is expected to replace
   whatever was there, `npm link`ed packages included.
3. Checks the cache root and `<dir>` are on the same filesystem (compares
   device ids) and errors clearly *before* any `npm ci` if not, since
   hardlinks can't cross filesystems. (An `EXDEV` from an individual
   `link` call later on also produces a clear message.)
4. If no cache entry exists for that key yet: builds one in
   `<cache root>/node-cache/<key>.tmp-<pid>/` — copies `package-lock.json`
   *and* `package.json` in (plus `<dir>/.npmrc`, if present, with mode
   `0600`, so a project-level registry/auth/`legacy-peer-deps` setting
   applies) and runs `npm ci` there (npm's output goes to stderr so you see
   its progress) — then atomically renames it to
   `<cache root>/node-cache/<key>/`. The staged inputs are re-keyed first;
   if the lockfile or `.npmrc` changed since step 1, it errors (rerun)
   rather than file the entry under the wrong key. The staged `.npmrc` is
   deleted as soon as `npm ci` finishes, successfully or not, so a registry
   token never persists in the cache.

   **Unsupported: npm workspaces.** If `package.json` declares a non-empty
   `workspaces` field, `ensure` fails before running anything, since the
   workspace packages aren't available to `npm ci` in the staging dir; run
   `npm ci` in the worktree yourself instead. A `package.json` that isn't a
   valid JSON object is likewise a clear error.

   **Interrupts:** SIGINT/SIGTERM (e.g. Ctrl-C) while the entry is being
   built sends `npm ci` SIGTERM (killed after 10s if still running),
   removes the staging dir and exits 1. A second Ctrl-C kills fleet-cache
   at once, skipping that cleanup (`gc` then reports the staging dir once
   it is a day old). Interrupting the later hardlink step just leaves a
   `node_modules` without the marker, which the next `ensure` rebuilds.
   Every regular file in the resulting `node_modules` is then chmod'd
   read-only (`0444`) — since permission bits belong to the shared inode,
   not the directory entry, this takes effect for every worktree the file
   is later hardlinked into, not just this cache copy. Directories stay
   writable, so entries can still be added/removed/replaced (this is how
   `ensure`'s own rebuild and `npm link`'s symlink-swap both keep working);
   only in-place edits to existing file *content* are blocked, turning what
   would otherwise be silent corruption shared across every worktree using
   that cache entry into a loud permission error instead.
5. If `<dir>/node_modules` already exists (and step 2 didn't already skip),
   removes it (it's a disposable derived artifact).
6. Hardlinks the cache entry's `node_modules` tree into `<dir>/node_modules`:
   regular files become hardlinks (`os.Link`); symlinks (e.g. npm bin shims)
   are recreated as symlinks rather than hardlinked. Writes the
   `.fleet-cache-hash` marker used by step 2's skip check.
7. Prints `linked node_modules from cache <key8chars>` (to stderr) on success.

Intended to be called by `fleet-task new` after creating each repo's
worktree, per the CLI contract — a cache failure for one repo should not
abort the whole `fleet-task new` run.

### `fleet-cache gc [--roots <dir>[,<dir>...]] [--force] [--json] [--cache-root <dir>]`

Garbage-collects cache entries that are no longer referenced by any known
`package-lock.json` (+ `.npmrc`).

- `--roots`: one or more directories (flag may be repeated, and/or each
  value may be comma-separated) to scan recursively for `package-lock.json`
  files. For every lockfile found, the [cache key](#cache-key) of its
  directory (that lockfile plus the `.npmrc` beside it, if any) counts as
  referenced; any cache entry whose key isn't in that set is considered
  stale, including entries named by the old lockfile-only key. (A file
  symlink with another name that points at a `package-lock.json` is keyed
  in its target's directory.) (`node_modules`
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
- Also flags abandoned `<key>.tmp-<pid>` staging dirs older than a day
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
`~/.cache/fleet`. Entries live at `<cache root>\node-cache\<key>\`, with
the same [cache key](#cache-key) as `ensure`.

Same skip-marker and `--force` semantics as `ensure`: if
`<wsl-dir>/node_modules/.fleet-cache-hash` already matches the cache
key, nothing is touched (so `npm link`s survive reruns); `--force` rebuilds
(discarding manual changes). Cache entries are built via the same
staging-dir-and-rename scheme as `ensure`, with `npm ci` run natively in the
staging dir.

Like `ensure`, it stages `<wsl-dir>/.npmrc` for `npm ci` and removes it
afterwards, rejects `workspaces`, and cleans up its staging dir on
SIGINT/SIGTERM (stopping the `powershell.exe` running `npm ci`; whether the
Windows-side npm stops with it is up to WSL interop, so the cleanup's
`Remove-Item` can fail on locked files, leaving the staging dir for `gc`).

Only genuinely bulk filesystem work — `npm ci`, removing an old
`node_modules` tree, and hardlink-populating the target `node_modules` —
runs as native Windows processes (PowerShell/npm). Single-file operations
(computing the key, copying `package.json`/`package-lock.json`/`.npmrc`
into the cache entry, the skip marker) stay as ordinary Go file I/O against the `/mnt/...` path, since
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
`New-Item -ItemType HardLink` fails across volumes. A path that resolves to
no drive letter at all (e.g. `\\wsl.localhost\...`, i.e. inside the WSL
filesystem) is rejected with its own error.

PowerShell failures propagate: the generated commands abort if `cd` fails
and exit non-zero if a cmdlet such as `Remove-Item` fails (e.g. a locked
file), rather than reporting success.

### `fleet-cache gc-windows [--roots <dir>[,<dir>...]] [--force] [--json] [--cache-root <wsl-path>]`

Windows-native counterpart of `gc` — shares its core (same `--roots`
validation, `--force`/`--json` rules, staging-dir cleanup; no `--roots` ⇒
always report-only), scanning/deleting against the
Windows-native cache root's `node-cache` dir via `Remove-Item -Recurse -Force`
instead of `os.RemoveAll`.

### `fleet-cache list [--json] [--cache-root <dir>]`

Read-only listing of `<cache root>/node-cache`, sorted by name: every cache
entry (`kind: entry`) with whether it is complete (has the
`.fleet-cache-complete` marker; an incomplete one is rebuilt by the next
`ensure`), and every `<key>.tmp-<pid>` staging dir (`kind: staging`,
`abandoned` once older than a day, i.e. what `gc` would remove). Each row
has the directory's last-modified time and its on-disk size (allocated
blocks, like `du`; the files are hardlinked into worktrees, so deleting an
entry frees that space only once no worktree still links them).

Text output is a table with a total row; `--json` prints
`{"node_cache": ..., "entries": [{"name", "path", "kind": "entry"|"staging",
"complete": bool, "abandoned": bool, "modified": RFC 3339 time,
"size_bytes": int}]}`. A missing `node-cache` dir is an empty listing.

There is no `list-windows`: pass the WSL-visible Windows cache root (e.g.
`--cache-root /mnt/c/Users/<you>/AppData/Local/fleet`) to list that cache.
Sizing walks every file, which is slow across the WSL 9P boundary for a
large Windows cache.

## Development

From the repo root (one module for all three tools):

```
go build ./... && go vet ./... && go test ./... && gofmt -l .
go build -ldflags "-X main.version=v1.2.3" ./cmd/fleet-cache   # stamp a release version
```

Tests avoid running real `npm ci` (no network calls in `go test`); the
`ensure` cache-hit and hardlink-walk logic is exercised against
pre-populated fake directories instead.
