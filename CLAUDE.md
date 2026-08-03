# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`fleet` is three independent Go CLIs — `fleet-task`, `fleet-run`, `fleet-cache` — that let one
person run several Jira tickets in parallel, each in its own set of git worktrees, each with its
own subset of running applications, without their working trees or running processes conflicting.
The underlying problem: a programmer works Jira tickets that each touch some subset of a large
repo suite (backend, database, several web UIs); moving a ticket to "in progress" means checking
out branches across whichever repos it needs, testing with the apps running, then pushing up and
moving it to "in review". Doing that with AI agents driving several tickets at once requires
isolated working directories and isolated running-app sets per ticket, since a single shared
checkout/process set per repo can't be in two branches or two states at once.

See `docs/CONTRACT.md` for the authoritative shared contract (paths, `repos.yaml` schema, state
file format, hook/tmux naming conventions) — read that file before changing anything that crosses
module boundaries.

### Design constraints that shaped this

- **Terminal-only**: every workflow is a command or a TUI (fzf pickers); no GUI.
- **Node-ready**: worktrees get `npm install`ed automatically (via `fleet-cache`) so they're usable
  immediately, not just checked out.
- **WSL2-aware**: some apps must run under native Windows, not WSL2 Linux — see the
  `runtime: linux|windows` split below.
- **Space-saving & fast**: `node_modules` across many worktrees of the same repo/lockfile are
  hardlinked from one shared cache rather than reinstalled each time; git history is shared via one
  bare clone per repo rather than a full clone per ticket.
- **Fully configurable**: which repos, which directories, and how the working directory is
  structured all come from `repos.yaml` / env vars, not hardcoded assumptions — see
  `docs/CONTRACT.md`.
- **XDG-compliant**: config/state/cache each live under their own XDG base dir, all overridable.
- **UNIX-y / no shared runtime**: three separate precompiled Go binaries gluing together common
  CLI tools (`git`, `tmux`, `fzf`, `npm`) rather than one monolith with a language runtime
  dependency; commands support `--json`/plain-stdout output so they compose with `jq`/`fzf`/shell
  pipelines instead of only working interactively.

Each subdirectory (`fleet-task/`, `fleet-run/`, `fleet-cache/`) is its own Go module with its own
`go.mod`. **There is no shared Go code between them** — this is intentional (each may become its
own git repo later). They coordinate only through:
1. Files on disk: a user-authored `repos.yaml` config, and per-ticket state files at
   `tasks/<ticket>.json` (one file per ticket, never a single shared file — parallel tickets must
   never contend on the same file).
2. Shelling out to each other by binary name on `PATH` (e.g. `fleet-task new` calls
   `fleet-cache ensure <dir>`).

Changing the on-disk contract (config schema, state file shape, hook env vars, tmux window naming)
means updating `docs/CONTRACT.md` and checking all three modules, not just one.

## Commands

Build/test/vet each module independently from within its directory (there is no root-level
`go.work` — treat `fleet-task/`, `fleet-run/`, `fleet-cache/` as separate projects):

```sh
cd fleet-task && go build ./... && go vet ./... && go test ./...
cd fleet-run   && go build ./... && go vet ./... && go test ./...
cd fleet-cache && go build ./... && go vet ./... && go test ./...
```

Run a single test:

```sh
cd fleet-task && go test ./... -run TestName -v
```

`go build ./...` from inside a module directory drops a binary named after the module in that same
directory (e.g. `fleet-task/fleet-task`) — this is a build artifact, not a committed file; remove
it (`rm fleet-task/fleet-task`) rather than leaving it around or committing it.

Shell integration tests (requires [bats-core](https://github.com/bats-core/bats-core)):

```sh
cd fleet-task/contrib && bats fleet.bats fzf_integration.bats
```

`fzf_integration.bats` builds a real `fleet-task` binary and drives it against a stubbed `fzf` on
`PATH` (a script that just `cat`s or `head -n1`s stdin) so the real stdin/stdout/stderr wiring
between the Go binary and the `fzf` subprocess is exercised headlessly, without needing a TTY.

There is no CI config in this repo yet — the commands above are the full verification loop.

## Architecture

### The `runtime: linux|windows` split

`repos.yaml` marks each repo `runtime: linux` or `runtime: windows`. This field changes how both
`fleet-run` and `fleet-cache` execute, not just what they run:
- `linux` repos: commands run directly; `fleet-cache` hardlinks `node_modules` from its cache.
- `windows` repos: `fleet-run` translates the worktree path with `wslpath -w` and wraps the run
  command as `powershell.exe -NoExit -Command "cd '<win-path>'; <cmd>"` instead of using tmux's
  `-c` (which only understands WSL-side paths). Hardlinks can't cross the WSL9P boundary, so
  Windows-side `node_modules` caching is out of scope for `fleet-cache` in its current form — see
  the note in `fleet-cache/ensure.go`.

### `fleet-task`: bare-clone-once, worktree-per-ticket

Each repo in `repos.yaml` has an `origin` (remote URL) and a `base` (a local **bare** clone path
shared across all tickets). `fleet-task new` clones `base` once (first use) and only `fetch`es
thereafter; every ticket gets its own `git worktree add` off that same `base`, on a branch named
after the ticket. This is why `fleet-task/git.go`'s `gitCloneBare` explicitly sets
`remote.origin.fetch` to `+refs/heads/*:refs/remotes/origin/*` and does an initial fetch right
after cloning — a bare clone does *not* set that refspec on its own, so `origin/<branch>` refs
would otherwise never exist and every subsequent worktree add would fail (this was a real bug
caught during end-to-end testing, not a hypothetical).

Command flow for `fleet-task new` (see `cmd_new.go`): prompt for ticket/description → fzf-multiselect
repos → per repo, fzf-multiselect patches (`<config dir>/patches/<repo>/*.patch`, skipped entirely
if none exist) → ensure `base` exists/is fetched → run `pre-create` hooks → `git worktree add` →
apply patches with `git apply` (not `git am` — see README for why) → run `post-create` hooks → if
`package-lock.json` exists, shell out to `fleet-cache ensure <worktree>` (non-fatal on failure) →
write `tasks/<ticket>.json`. Each of these steps is a separately named function so the pure-logic
pieces (config parsing, state file round-trip, hook discovery/ordering, SSH URL detection) are unit
tested without needing real git/fzf/npm; the subprocess-driving code (`git.go`, `fzf.go`, `cache.go`)
is intentionally left to manual/integration testing.

Per-ticket state writes go through an advisory lock (`lock.go`): a `<file>.lock` sibling created
with `O_EXCL`, not a real `flock(2)` — deliberate, since per-ticket files already make cross-ticket
contention impossible by construction; the lock only guards the rare same-ticket race.

### `fleet-run`: one fixed tmux session, windows do the rest

There is exactly one tmux session for the whole fleet (`repos.yaml`'s `tmux.session_name`) —
**never** renamed or recreated per ticket. Every ticket's running apps live as windows inside that
same session, named `<ticket>-<repo>-<run-name>`. All lifecycle logic (`start`/`stop`/`switch`)
keys off this window-naming pattern, never off session identity. Because ticket ids, repo names,
and run names can all contain hyphens, exact reverse-parsing of a window name back into its three
parts is ambiguous in general (`window.go`'s `parseWindowName` resolves it via longest-prefix
matching against a caller-supplied set of known tickets/repos); the operations that actually matter
(`stop`, `switch`) only need unambiguous ticket-*prefix* filtering, not full decomposition, and use
`hasWindowPrefix`/`filterWindowsByTicketPrefix` instead.

`fleet-run start` requires either `--ticket <id>` or exactly one file under `tasks/*.json` (there's
no "plain checkout, no ticket" mode). `switch --to <ticket>` is implemented as an in-process call to
`killAllWindowsInSession` followed by the same `startFlow` used by `start` — not a re-exec of its
own binary.

Command-building for every `tmux` invocation shape lives in pure, separately tested functions in
`tmux_cmd.go` (argv construction only); the actual subprocess execution is a thin, deliberately
untested wrapper in `tmux_exec.go`.

### `fleet-cache`: content-addressed hardlink cache

Cache key is the sha256 of a repo's `package-lock.json`. `fleet-cache ensure <dir>` populates
`<cache root>/node-cache/<hash>/` (copies both `package-lock.json` *and* `package.json` in before
running `npm ci` — `npm ci` needs both present, copying only the lockfile fails with an ENOENT on
`package.json`; another bug caught during end-to-end testing) once per hash, then hardlinks that
cached `node_modules` tree into the target directory on every call (regular files via `os.Link`;
symlinks, e.g. npm bin shims, recreated as symlinks rather than hardlinked, via `hardlink.go`'s
`hardlinkTree`). `fleet-cache gc` only ever deletes when given both `--roots` (directories to scan
for still-referenced lockfiles) and `--force`; omitting either makes it report-only, so a bare
`fleet-cache gc` can never wipe the cache by accident.

## Cross-tool contract (change with care)

Defined authoritatively in `docs/CONTRACT.md`; summarized here as a reminder of what's shared:

- XDG paths, all overridable via `FLEET_CONFIG_DIR` / `FLEET_REPOS_FILE` / `FLEET_STATE_DIR` /
  `FLEET_CACHE_DIR` env vars — `fleet-task` and `fleet-run` each re-implement the same resolution
  order (`paths.go` in both) rather than sharing code; keep them in sync by hand if it changes.
  This is also how tests and the E2E flow sandbox a fake environment without touching `~/.config`
  or `~/.local/state`.
- `repos.yaml` shape (`tmux`, `worktree_root`, and per-repo `origin`/`base`/`default_branch`/
  `runtime`/`run`) — `fleet-task/config.go` and `fleet-run/config.go` both parse this file
  independently and must stay structurally compatible.
- `tasks/<ticket>.json` shape — written by `fleet-task` (`state.go`), read by both `fleet-task`
  and `fleet-run` (`task.go`).
- Hook env vars (`FLEET_TICKET`, `FLEET_REPO`, `FLEET_WORKTREE_DIR`) passed to executables under
  `<config dir>/hooks/{pre-create,post-create,pre-run,post-run}/*`.
