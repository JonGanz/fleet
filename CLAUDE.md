# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`fleet` is three Go CLIs — `fleet-task`, `fleet-run`, `fleet-cache` — that let one
person run several tickets (from any issue tracker — Jira, GitHub issues, … — or none) in parallel, each in its own set of git worktrees, each with its
own subset of running applications, without their working trees or running processes conflicting.
There is no issue-tracker integration: a ticket id is an opaque string (validated only to be safe as
a filename) — keep it that way.
The underlying problem: a programmer works tickets that each touch some subset of a large
repo suite (backend, database, several web UIs); moving a ticket to "in progress" means checking
out branches across whichever repos it needs, testing with the apps running, then pushing up and
moving it to "in review". Doing that with AI agents driving several tickets at once requires
isolated working directories and isolated running-app sets per ticket, since a single shared
checkout/process set per repo can't be in two branches or two states at once.

See `docs/CONTRACT.md` for the authoritative shared contract (paths, `repos.yaml` schema, state
file format, hook/tmux naming conventions) — read that file before changing anything that crosses
tool boundaries.

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
  Every command except `fleet-run attach` can run without a terminal: flags replace every picker/prompt, a missing value
  exits 2 (no terminal on stdin, or `FLEET_NO_INPUT=1`), and `--json` is on every fleet-task and
  fleet-run command (except `fleet-task jump`, `fleet-run attach` and the `version` commands) plus
  `fleet-cache gc`/`gc-windows`/`list`; `fleet-task jump` prints a bare path. All three tools exit
  0 ok / 1 failed / 2 usage error or input required, and print every diagnostic on stderr as
  `<tool>: error: <msg>` or `<tool>: warning: <msg>` (`internal/cli`).
  `skills/fleet/SKILL.md` is the agent-facing guide built on that surface.

The repo is one Go module (`github.com/JonGanz/fleet`, `go.mod` at the root). Each binary is a
`package main` under `cmd/` (`cmd/fleet-task/`, `cmd/fleet-run/`, `cmd/fleet-cache/`); code more
than one of them needs lives in `internal/`, one package per concern:

- `internal/cli` — diagnostics (`Errorf`/`Warnf` with the `<tool>:` prefix), `UsageError` and the
  0/1/2 exit-code mapping (`ExitCode`, `Report`), `InteractiveAllowed`, version resolution,
  `PrintJSON`.
- `internal/xdg` — config/state/cache directory resolution (`FLEET_*` overrides, XDG variables,
  defaults), `ExpandHome`.
- `internal/config` — `repos.yaml` types, `Load`/`Parse`, `defaults`, the validation every tool
  needs.
- `internal/taskstate` — `tasks/<ticket>.json`: types, `Read`/`List`/`Write`, `schema_version`,
  ticket-id validation.
- `internal/wsl` — `wslpath` wrappers, PowerShell quoting and command scripts, the same-drive
  check.
- `internal/picker` — the bubbletea list picker.
- `internal/hooks` — hook discovery, env and running (pre-* veto, timeout).
- `internal/proc` — running a child with a timeout, optionally in its own process group (killed as
  a group; SIGINT/SIGTERM/SIGHUP forwarded, then re-raised), for hooks and network git.
- `internal/tmuxsock` — the `FLEET_TMUX_SOCKET` `-L` argument.

Sharing code does not change how the binaries coordinate at runtime; they still talk only through:
1. Files on disk: a user-authored `repos.yaml` config, and per-ticket state files at
   `tasks/<ticket>.json` (one file per ticket, never a single shared file — parallel tickets must
   never contend on the same file).
2. Shelling out to each other by binary name on `PATH` (e.g. `fleet-task new` calls
   `fleet-cache ensure <dir>`). Keep it that way: don't turn these calls into function calls or
   merge the binaries.

Behaviour that only one tool needs stays in that tool (e.g. fleet-run's tmux window-name
validation in `cmd/fleet-run/config.go`), so a shared package change doesn't silently tighten the
other tools. Changing the on-disk contract (config schema, state file shape, hook env vars, tmux
window naming) means updating `docs/CONTRACT.md` and checking every binary, not just one.

## Commands

Build/test/vet everything from the repo root:

```sh
go build ./... && go vet ./... && go test ./...
```

Run a single test:

```sh
go test ./cmd/fleet-task -run TestName -v
```

The root `Makefile` wraps this: `make check` (gofmt check + `go vet` + `go test -race`),
`make build` (version-stamped binaries into `dist/`), `make lint` (staticcheck, skipped if not
installed), `make install`/`uninstall` (via `install.sh`), `make clean`.

`go build ./...` writes no binaries for multiple packages, but `go build ./cmd/fleet-task` (from
the root) or `go build` inside `cmd/fleet-task/` drops a `fleet-task` binary in the current
directory — a build artifact, not a committed file (`.gitignore` ignores it); remove it (or
`make clean`) rather than leaving it around.

`./install.sh` (needs `go` on `PATH`) builds all three with `-trimpath`, stamping `main.version`
(`$FLEET_VERSION`, else `git describe --tags --always --dirty`, else `dev`), and installs each
atomically (temp file + `mv`) into `~/.local/bin` (or `$BIN_DIR`); it copies `skills/fleet/` to
`$XDG_DATA_HOME/fleet/skills/fleet` and symlinks `$CLAUDE_CONFIG_DIR/skills/fleet` (default
`~/.claude/skills/fleet`) to that copy — never to the checkout, so the skill matches the installed
binaries. `./install.sh --uninstall` reverses it.

The bubbletea pickers have no headless integration test; their state machines are unit tested by
feeding synthetic key messages.

CI (`.github/workflows/ci.yml`) runs from the root: gofmt check, build, vet, `go test -race` (with
tmux installed, so fleet-run's integration tests run), staticcheck and govulncheck, plus
shellcheck on `install.sh`; dependabot updates the Go module and actions weekly. Locally,
`make check` (plus `make lint` if staticcheck is installed) is the verification loop.

## Architecture

### The `runtime: linux|windows` split

`repos.yaml` marks each repo `runtime: linux` or `runtime: windows`. This field changes how all
three tools execute, not just what they run:
- `linux` repos: git and commands run directly in WSL/Linux; `fleet-cache ensure` hardlinks
  `node_modules` from its cache (`<cache dir>/node-cache/<key>`).
- `windows` repos: the bare clone and worktree live on an NTFS volume (`windows_base`,
  `windows_worktree_root`) and are driven by Windows-native `git.exe` via `powershell.exe`; the
  normal WSL-side worktree path is a **symlink** to the real tree. `fleet-run` resolves that symlink
  (`filepath.EvalSymlinks`), translates the result with `wslpath -w`, and wraps the run command as
  `powershell.exe -NoExit -Command "Set-Location -LiteralPath '<win-path>'; <cmd>"` instead of using tmux's `-c` (which
  only understands WSL-side paths). `fleet-cache ensure-windows`/`gc-windows` keep a separate
  Windows-native cache (hardlinks can't cross the WSL 9P boundary) and do the bulk work
  (`npm ci`, hardlink population via `hardlink_windows.ps1`) in native PowerShell/npm processes.
  NTFS hardlinks need one drive: `fleet-task` checks base vs worktree root, `fleet-cache` checks
  cache root vs target (`wsl.CheckSameDrive`).

### `fleet-task`: bare-clone-once, worktree-per-ticket

Each repo in `repos.yaml` has an `origin` (remote URL) and a `base` (a local **bare** clone path
shared across all tickets). `fleet-task new` clones `base` once (first use) and only `fetch`es
thereafter; every ticket gets its own `git worktree add` off that same `base`, on a branch computed
from `branch_template` (default: the ticket id). `ensureBase` (`gitrunner.go`) does
`git init --bare` + `remote add origin` + `fetch --prune origin` instead of `git clone --bare`:
`clone --bare` copies every remote branch into local `refs/heads/*` (stale clutter, since later
fetches only update `origin/*`) and doesn't set `remote.origin.fetch`. `remote add` sets the standard
`+refs/heads/*:refs/remotes/origin/*` refspec, so the base has *no* local branches until a worktree
creates one. `ensureBase` is idempotent (`planBaseSetup`): it completes a base left half-made by an
interrupted first run, refuses a non-empty non-repo directory, and only warns (never rewrites) when
the base's `origin` URL differs from `repos.yaml`. `planWorktreeAdd` then picks the branch: local
branch exists → attach it; else `origin/<branch>` exists → create a local branch tracking it; else
create from `origin/<default_branch>` with `--no-track` (so a plain `git push` can't target the
default branch). Network git (`fetch`) is killed after `git.network_timeout` (default 10m,
`internal/proc`; own process group only without a terminal, so ssh/HTTPS prompts keep working on
one), and without a terminal WSL-side fetch gets `GIT_SSH_COMMAND="ssh -o BatchMode=yes"` unless
`GIT_SSH_COMMAND`/`GIT_SSH`/`core.sshCommand` is set (`batchModeSSHEnv`; never for Windows
`git.exe`).

How git is driven is behind the `gitRunner` interface (`gitrunner.go`; `linuxGit` in `git.go`,
`windowsGit` in `gitwin.go`, PowerShell runners in `winpath.go`, built on `internal/wsl`). `new`/`edit`/`rm`/`mv` go through
it instead of switching on `repo.Runtime`; keep new git operations on the interface.

Command flow for `fleet-task new` (`cmd_new.go`): prompt for ticket/description (bubbletea form) →
validate the ticket id and **refuse if `tasks/<ticket>.json` already exists** → pick repos
(bubbletea multiselect, or `--repo` flags, which also switch patches to
`--skip-patch`/`--no-patches`, default all) → per repo, pick patches (`<config dir>/patches/<repo>/*.patch`, all
prompts up front, skipped if none) → then per repo, in `createRepoWorktree`: run `pre-create`
hooks (a failing one vetoes the repo) → `Preflight` + `ensureBase` → `InspectWorktreePath` +
`decideAdoption`: a leftover worktree of the base on the expected branch is adopted (no
`worktree add`, no patches, `patches: null`), anything else at the path is an error → else
`addWorktree` (attach / track / create, above) → apply patches with `git apply` (not `git am` —
see fleet-task README) → run `post-create` hooks → if `package-lock.json` exists, shell out to
`fleet-cache ensure[-windows]` (non-fatal on failure) → write `tasks/<ticket>.json` (after *each*
successful repo, so an interrupted `new` leaves a task that `edit --add` can finish, adopting the
repo it was working on). Every repo's branch name is validated (`validateBranchNames`) before
any work. `edit` reuses `createRepoWorktree` for added repos and the same teardown as `rm` for
removed ones. Every repo is attempted even after a failure; commands exit 1 if any failed.
`rm`/`mv`/`edit`-removal refuse a ticket that `fleet-run` currently has active (`tmux_check.go`
reads `@fleet_task_ticket`, honouring `FLEET_TMUX_SOCKET`). `rm` and `edit --remove` also refuse a
worktree with uncommitted changes beyond fleet's own applied patches, or one that can't be
checked (`changes.go`: `checkRemovable`), unless `--force` — a `[y/N]` prompt on a terminal,
exit 2 without one; `show` and `list --status` report the same check. A failing `pre-remove` hook
vetoes that repo's removal. After a worktree is removed, `cleanupBranch` (`cmd_rm.go`) deletes its
local branch from the base only if `rev-list --count <branch> --not --remotes` is 0 (else keeps it
and says why; `branch_deleted`/`branch_kept_reason` in JSON); `--keep-branch` keeps it,
`--delete-branch` deletes regardless after `checkBranchDeletion`'s up-front `y/N` (exit 2 without
a terminal unless `--force`). Hooks (`internal/hooks`) are killed after `hooks.timeout` (default
5m), as a process group; pre-* failures veto the one repo/window, post-* failures warn.

Subcommand flag parsing is per-subcommand `flag.FlagSet`s in `args.go` (flags and positionals may
interleave; `--help` per command). fleet-task-wide helpers (`requireNotActive`, config loading,
`lockAndReadTask`) are in `common.go`, `taskFile`/`worktreeRoot` in `paths.go`; ticket-id
validation is `taskstate.ValidateTicket`. The pickers are `internal/picker` (`picker.Run`) and
`picker.go` (`pickTicket`, built on it); `textinput.go` is the ticket/description form.

Every mutating command (`new`/`edit`/`rm`/`mv`) holds the ticket's advisory lock (`lock.go`,
`lockTicket`) for its whole run, taken before the state is read; `mv` locks both tickets in sorted
order. It's a `<file>.lock` sibling created with `O_EXCL` containing the owner pid, not a real
`flock(2)` — deliberate, since per-ticket files already make cross-ticket contention impossible by
construction; the lock only guards the rare same-ticket race. It fails fast (exit 1, "locked by
another fleet-task") instead of waiting. A live pid's lock is never taken over; a dead pid's is; one
with no readable pid is once older than a minute. Takeover renames the stale file aside first, so
two processes can't both take over. `fleet-task unlock <ticket>` (`cmd_unlock.go`) removes a lock
by the same rule (refusing a live owner unless `--force`); the "locked" error points at it.

Unit-tested (in `cmd/fleet-task` or the `internal/` package that owns the code): config parsing,
state round-trip, locking, hook discovery/ordering/env, SSH URL detection, arg parsing, branch
templates, picker state machines, PowerShell script builders.
Tested against real temp git repos (skipped without `git`): base setup and worktree-add modes
(`git_test.go`), the changes check and removal guard (`changes_test.go`), `new`/`mv` flows
(`e2e_test.go`), hook vetoes, adoption, branch cleanup and the fetch timeout
(`lifecycle_test.go`), `unlock` (`cmd_unlock_test.go`). Deliberately left to manual testing: the Windows runner (`gitwin.go`),
`ssh-add`, the `fleet-cache` shell-out (`cache.go`) and the picker's terminal wiring.

### `fleet-run`: one fixed tmux session, only one ticket live at a time

There is exactly one tmux session for the whole fleet (`repos.yaml`'s `tmux.session_name`) —
**never** renamed or recreated per ticket. Only one ticket's app set is ever meant to be running in
it at once — the point of `fleet-run` is switching the whole runtime environment to a different
work context, not stacking several tickets' windows side by side. Each running app is a window
named `<repo>-<run-name>` (no ticket prefix — since only one ticket runs at a time, it doesn't need
to be disambiguated in the name). The picker/CLI form is `repo:run-name`; `config.go`'s
`validateRunConfig` (run after the shared `internal/config` load) therefore rejects `:` in repo/run
names and any two `<repo>-<run>` window names that collide.

Commands: `start`, `restart`, `stop`, `status`, `targets`, `logs`, `attach` (`cmd_*.go`; `restart`
shares `start`'s code), plus `version`; all but `attach` and `version` take `--json`. `attach` is
for humans (needs a terminal; `switch-client` inside the same tmux server, else `attach-session`).
`start`/`restart`/`stop` take `repo:run-name` positionals or `--all` instead of the multiselect.
Windows get `remain-on-exit on` (chained against the creation target, never the window name), so a
crashed app is a dead pane that `status` reports, `logs` reads (`capture-pane`) and `start`
respawns; `restart` respawns live panes too. Existing windows are always targeted by tmux window
id (`@N`), never by name, so dots in repo/run names work; `config.go` rejects `.`/`:` in
`tmux.session_name`. `pre-run`/`post-run` hooks (`hooks.go`, on `internal/hooks`) run around each
window created or respawned; a failing `pre-run` hook cancels that window (reported `failed`). `FLEET_TMUX_SOCKET` makes every tmux call use `-L <value>`. `fleet-run start [--ticket <id>]` resolves the ticket: `--ticket`, else the only file under
`tasks/*.json`, else a picker when there are several (zero is an error; there's no "plain
checkout, no ticket" mode). "Which ticket is active" is tracked via two tmux user options set with
`-g` (global, `activeticket.go`; not per-session, because `set-option -t =<session>` can't resolve
a bare session target without an attached client), `@fleet_task_ticket`/`@fleet_task_description`
— set on `start` right after the first window exists (tmux needs a running server, and
`set-option` doesn't start one), cleared once `stop` leaves nothing running. If the session dies
outside fleet-run while the server survives, the record goes stale: `status` reports
`stale_active`, and any `stop` with no windows to kill clears it. Starting the same
ticket that's already active is additive (only missing windows get created); starting a *different*
ticket kills every window in the session first (`killAllWindowsInSession`), then runs the normal
create flow. `stop --ticket X` uses the option purely as a safety assertion (errors out, stopping
nothing, if `X` isn't the active ticket) rather than as a filter.

The decisions are pure, tested helpers: `active_logic.go` (`needsSwitch`, `shouldClearActive`),
`stop_logic.go`, `pairs.go`; per-subcommand flag parsing (flags and positionals interleaved) lives in `cmdutil.go`. Command-building
for every `tmux` invocation shape lives in `tmux_cmd.go` (argv construction only, tested; targets
use exact-match `=<session>:<index>` forms, or window ids); `tmux_exec.go` is the thin subprocess
wrapper. `integration_test.go` drives the real commands against a private tmux server
(`FLEET_TMUX_SOCKET`, temp config/state; skipped without tmux). Its pickers are `internal/picker`
(`pickers.go` wraps it as `selectOne`/`selectMulti`).

### `fleet-cache`: content-addressed hardlink cache

Cache key is a sha256 over a versioned encoding of the sha256 of `package-lock.json` and of the
sibling `.npmrc` (or `none`) — `cacheKey` in `hash.go`, the one function ensure, ensure-windows and
gc all use. `fleet-cache ensure <dir>` populates
`<cache root>/node-cache/<key>/` once per key (copies both `package-lock.json` *and*
`package.json` in before running `npm ci` — `npm ci` needs both present, copying only the lockfile
fails with an ENOENT on `package.json`; another bug caught during end-to-end testing), then
hardlinks that cached `node_modules` tree into the target directory (regular files via `os.Link`;
symlinks, e.g. npm bin shims, recreated as symlinks, via `hardlink.go`'s `hardlinkTree`). Entries
are built in `<key>.tmp-<pid>` staging dirs and renamed into place (`entry.go`), so a killed
`npm ci` never leaves a reusable half-built entry and concurrent ensures don't corrupt each other.
`<dir>/node_modules/.fleet-cache-hash` records the linked key, making repeat `ensure` a no-op
(preserves hand-made `npm link`s) unless the lockfile or `.npmrc` changed or `--force` is given. On Linux,
`samedev_unix.go` fails early if cache and target are on different devices. The worktree's
`.npmrc` is staged (`0600`) for `npm ci` and deleted right after; it is part of the key only as a
hash, so no token is stored. A `package.json` with `workspaces` is rejected up front. SIGINT/SIGTERM during a build
stops npm and removes the staging dir (`interrupt.go`), exiting 1.

`fleet-cache gc` only ever deletes when given both `--roots` (directories to scan for
still-referenced lockfiles; every root must exist) and `--force`; omitting either makes it
report-only, so a bare `fleet-cache gc` can never wipe the cache by accident. It also flags stale
(>1 day) staging dirs. `fleet-cache list [--json]` is the read-only view of entries and staging
dirs. Progress and npm output go to stderr; stdout is results only. Exit codes are 0/1/2 like the
other two tools.

## Cross-tool contract (change with care)

Defined authoritatively in `docs/CONTRACT.md`; summarized here as a reminder of what's shared:

- XDG paths, all overridable via `FLEET_CONFIG_DIR` / `FLEET_REPOS_FILE` / `FLEET_STATE_DIR` /
  `FLEET_CACHE_DIR` env vars, resolved by `internal/xdg` for all three tools. A relative `FLEET_*`
  value is made absolute; a relative `XDG_*_HOME` is ignored with a warning (the XDG spec requires
  absolute paths). This is also how tests and the E2E flow sandbox a fake environment without
  touching `~/.config` or `~/.local/state`. `FLEET_TMUX_SOCKET` (tmux `-L`) is read by both `fleet-run` and
  `fleet-task`'s active-ticket check, and sandboxes tmux the same way.
- `repos.yaml` shape (`tmux`, `hooks.timeout`, `git.network_timeout`, `worktree_root`, `windows_*`, `defaults`, and per-repo `origin`/
  `base`/`default_branch`/`branch_template`/`runtime`/`run`) — parsed and validated once, by
  `internal/config`, for both `fleet-task` and `fleet-run` (`fleet-run` ignores `branch_template`
  and adds its own window-name checks).
- `tasks/<ticket>.json` shape — `internal/taskstate`; written by `fleet-task`, read by both
  `fleet-task` and `fleet-run`. The filename is authoritative for the ticket id in both (a
  disagreeing `ticket` field warns and is ignored). Every write stamps `"schema_version": 1`; a file
  without it is version 1, and a newer version is refused (list-style commands warn and skip it).
  Bump `taskstate.SchemaVersion` only for an incompatible change.
- Hook env vars (`FLEET_TICKET`, `FLEET_DESCRIPTION`, `FLEET_REPO`, `FLEET_BRANCH`,
  `FLEET_WORKTREE_DIR`, plus `FLEET_RUN_NAME` for run hooks) passed to executables under
  `<config dir>/hooks/{pre-create,post-create,pre-remove,post-remove,pre-run,post-run}/*`;
  fleet-task runs the create/remove phases, fleet-run the run phases. A failing pre-* hook vetoes
  its repo/window; every hook is killed after `hooks.timeout`.
