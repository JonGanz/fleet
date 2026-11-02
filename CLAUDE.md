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

- **Terminal-only**: every workflow is a command or an in-process bubbletea TUI picker (no `fzf`
  or other external picker binary); no GUI.
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
  CLI tools (`git`, `tmux`, `npm`) rather than one monolith with a language runtime dependency.
  Read commands that are meant to be scripted support `--json` (`fleet-task list`,
  `fleet-run status`, `fleet-cache gc`/`gc-windows`); `fleet-task jump` prints a bare path.

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

`./install.sh` builds all three into `~/.local/bin` (or `$BIN_DIR`).

Shell integration test (requires [bats-core](https://github.com/bats-core/bats-core)):

```sh
cd fleet-task/contrib && bats fleet.bats
```

`fleet.bats` sources `contrib/fleet.sh` and tests the `fj` function against a stub `fleet-task` on
`PATH`: it must `cd` to the path `jump` prints, and must *not* `cd` when `jump` exits non-zero
(cancelled picker) or prints a nonexistent path. The bubbletea pickers themselves have no headless
integration test; their state machines are unit tested by feeding synthetic key messages.

There is no CI config in this repo yet — the commands above are the full verification loop.

## Architecture

### The `runtime: linux|windows` split

`repos.yaml` marks each repo `runtime: linux` or `runtime: windows`. This field changes how all
three tools execute, not just what they run:
- `linux` repos: git and commands run directly in WSL/Linux; `fleet-cache ensure` hardlinks
  `node_modules` from its cache (`<cache dir>/node-cache/<hash>`).
- `windows` repos: the bare clone and worktree live on an NTFS volume (`windows_base`,
  `windows_worktree_root`) and are driven by Windows-native `git.exe` via `powershell.exe`; the
  normal WSL-side worktree path is a **symlink** to the real tree. `fleet-run` resolves that symlink
  (`filepath.EvalSymlinks`), translates the result with `wslpath -w`, and wraps the run command as
  `powershell.exe -NoExit -Command "cd '<win-path>'; <cmd>"` instead of using tmux's `-c` (which
  only understands WSL-side paths). `fleet-cache ensure-windows`/`gc-windows` keep a separate
  Windows-native cache (hardlinks can't cross the WSL 9P boundary) and do the bulk work
  (`npm ci`, hardlink population via `hardlink_windows.ps1`) in native PowerShell/npm processes.
  NTFS hardlinks need one drive: `fleet-task` checks base vs worktree root, `fleet-cache` checks
  cache root vs target (`checkSameDrive`).

### `fleet-task`: bare-clone-once, worktree-per-ticket

Each repo in `repos.yaml` has an `origin` (remote URL) and a `base` (a local **bare** clone path
shared across all tickets). `fleet-task new` clones `base` once (first use) and only `fetch`es
thereafter; every ticket gets its own `git worktree add` off that same `base`, on a branch computed
from `branch_template` (default: the ticket id). `gitCloneBare` (`git.go`) explicitly sets
`remote.origin.fetch` to `+refs/heads/*:refs/remotes/origin/*` and fetches right after cloning — a
bare clone does *not* set that refspec on its own, so `origin/<branch>` refs would otherwise never
exist and every worktree add would fail (a real bug caught during end-to-end testing).

How git is driven is behind the `gitRunner` interface (`gitrunner.go`; `linuxGit` in `git.go`,
`windowsGit` in `gitwin.go`, PowerShell wrappers in `winpath.go`). `new`/`edit`/`rm`/`mv` go through
it instead of switching on `repo.Runtime`; keep new git operations on the interface.

Command flow for `fleet-task new` (`cmd_new.go`): prompt for ticket/description (bubbletea form) →
validate the ticket id and **refuse if `tasks/<ticket>.json` already exists** → pick repos
(bubbletea multiselect) → per repo, pick patches (`<config dir>/patches/<repo>/*.patch`, all
prompts up front, skipped if none) → then per repo, in `createRepoWorktree`: run `pre-create`
hooks → `Preflight` + ensure `base` exists/is fetched → `git worktree add` (create branch, else
attach existing; both errors reported) → apply patches with `git apply` (not `git am` — see
fleet-task README) → run `post-create` hooks → if `package-lock.json` exists, shell out to
`fleet-cache ensure[-windows]` (non-fatal on failure) → write `tasks/<ticket>.json`. `edit` reuses
`createRepoWorktree` for added repos and the same teardown as `rm` for removed ones. Every repo is
attempted even after a failure; commands exit 1 if any failed. `rm`/`mv`/`edit`-removal refuse a
ticket that `fleet-run` currently has active (`tmux_check.go` reads `@fleet_task_ticket`).

Subcommand flag parsing is per-subcommand `flag.FlagSet`s in `args.go` (flags and positionals may
interleave; `--help` per command). Shared helpers (`requireNotActive`, config loading, ticket-id
validation in `taskFile`) are in `common.go`; the in-process pickers are `select.go` (`runSelect`)
and `picker.go` (`pickTicket`); `textinput.go` is the ticket/description form.

Per-ticket state writes go through an advisory lock (`lock.go`): a `<file>.lock` sibling created
with `O_EXCL` containing the owner pid, not a real `flock(2)` — deliberate, since per-ticket files
already make cross-ticket contention impossible by construction; the lock only guards the rare
same-ticket race. A lock whose pid is dead or that is older than 5 minutes is taken over.

Unit-tested: config parsing, state round-trip, locking, hook discovery/ordering, SSH URL detection,
arg parsing, branch templates, picker state machines, PowerShell script builders. Deliberately
left to manual testing: the subprocess-driving code (`git.go`, `gitwin.go`, `cache.go`) and the
picker's terminal wiring.

### `fleet-run`: one fixed tmux session, only one ticket live at a time

There is exactly one tmux session for the whole fleet (`repos.yaml`'s `tmux.session_name`) —
**never** renamed or recreated per ticket. Only one ticket's app set is ever meant to be running in
it at once — the point of `fleet-run` is switching the whole runtime environment to a different
work context, not stacking several tickets' windows side by side. Each running app is a window
named `<repo>-<run-name>` (no ticket prefix — since only one ticket runs at a time, it doesn't need
to be disambiguated in the name). The picker/CLI form is `repo:run-name`; `config.go`'s `validate`
therefore rejects `:` in repo/run names and any two `<repo>-<run>` window names that collide.

Commands: `start`, `stop`, `status [--json]` (`cmd_start.go`, `cmd_stop.go`, `cmd_status.go`).
`fleet-run start [--ticket <id>]` resolves the ticket: `--ticket`, else the only file under
`tasks/*.json`, else a bubbletea picker when there are several (zero is an error; there's no "plain
checkout, no ticket" mode). "Which ticket is active" is tracked via two tmux user options set with
`-g` (global, `activeticket.go`; not per-session, because `set-option -t =<session>` can't resolve
a bare session target without an attached client), `@fleet_task_ticket`/`@fleet_task_description`
— set on `start` right after the first window exists (tmux needs a running server, and
`set-option` doesn't start one), cleared once `stop` leaves nothing running. Starting the same
ticket that's already active is additive (only missing windows get created); starting a *different*
ticket kills every window in the session first (`killAllWindowsInSession`), then runs the normal
create flow. `stop --ticket X` uses the option purely as a safety assertion (errors out, stopping
nothing, if `X` isn't the active ticket) rather than as a filter.

The decisions are pure, tested helpers: `active_logic.go` (`needsSwitch`, `shouldClearActive`),
`stop_logic.go`, `pairs.go`; per-subcommand flag parsing (flags and positionals interleaved) lives in `cmdutil.go`. Command-building
for every `tmux` invocation shape lives in `tmux_cmd.go` (argv construction only, tested; targets
use exact-match `=<session>:<index>` forms); `tmux_exec.go` is the thin subprocess wrapper
(only its worktree-path helpers are unit tested). `select.go` is a deliberate hand-synced copy of
`fleet-task`'s picker.

### `fleet-cache`: content-addressed hardlink cache

Cache key is the sha256 of a repo's `package-lock.json`. `fleet-cache ensure <dir>` populates
`<cache root>/node-cache/<hash>/` once per hash (copies both `package-lock.json` *and*
`package.json` in before running `npm ci` — `npm ci` needs both present, copying only the lockfile
fails with an ENOENT on `package.json`; another bug caught during end-to-end testing), then
hardlinks that cached `node_modules` tree into the target directory (regular files via `os.Link`;
symlinks, e.g. npm bin shims, recreated as symlinks, via `hardlink.go`'s `hardlinkTree`). Entries
are built in `<hash>.tmp-<pid>` staging dirs and renamed into place (`entry.go`), so a killed
`npm ci` never leaves a reusable half-built entry and concurrent ensures don't corrupt each other.
`<dir>/node_modules/.fleet-cache-hash` records the linked hash, making repeat `ensure` a no-op
(preserves hand-made `npm link`s) unless the lockfile changed or `--force` is given. On Linux,
`samedev_unix.go` fails early if cache and target are on different devices.

`fleet-cache gc` only ever deletes when given both `--roots` (directories to scan for
still-referenced lockfiles; every root must exist) and `--force`; omitting either makes it
report-only, so a bare `fleet-cache gc` can never wipe the cache by accident. It also flags stale
(>1 day) staging dirs. Progress and npm output go to stderr; stdout is results only.

## Cross-tool contract (change with care)

Defined authoritatively in `docs/CONTRACT.md`; summarized here as a reminder of what's shared:

- XDG paths, all overridable via `FLEET_CONFIG_DIR` / `FLEET_REPOS_FILE` / `FLEET_STATE_DIR` /
  `FLEET_CACHE_DIR` env vars — `fleet-task` and `fleet-run` each re-implement the same resolution
  order (`paths.go` in both) rather than sharing code; keep them in sync by hand if it changes.
  This is also how tests and the E2E flow sandbox a fake environment without touching `~/.config`
  or `~/.local/state`.
- `repos.yaml` shape (`tmux`, `worktree_root`, `windows_*`, `defaults`, and per-repo `origin`/
  `base`/`default_branch`/`branch_template`/`runtime`/`run`) — `fleet-task/config.go` and
  `fleet-run/config.go` both parse this file independently and must stay structurally compatible
  (`fleet-run` ignores `branch_template`).
- `tasks/<ticket>.json` shape — written by `fleet-task` (`state.go`), read by both `fleet-task`
  and `fleet-run` (`task.go`); `created_at` is a `time.Time` in both.
- Hook env vars (`FLEET_TICKET`, `FLEET_REPO`, `FLEET_WORKTREE_DIR`) passed to executables under
  `<config dir>/hooks/{pre-create,post-create,pre-remove,post-remove,pre-run,post-run}/*`.
