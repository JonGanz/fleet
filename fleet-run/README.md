# fleet-run

`fleet-run` manages the tmux windows that run each task's applications, per
the shared contract in [`../docs/CONTRACT.md`](../docs/CONTRACT.md). It is a
standalone Go module (module path `github.com/JonGanz/fleet/fleet-run`) with no dependency on
`fleet-task` or `fleet-cache` Go code — it only reads the on-disk files those
tools produce (`repos.yaml`, `tasks/<ticket>.json`).

## The fixed-session / one-ticket-at-a-time model

There is exactly **one** tmux session for the whole fleet, named by
`repos.yaml`'s `tmux.session_name` (e.g. `fleet`). It is created once, on
first use, and is **never** renamed or recreated per ticket.

**Only one ticket's app set is ever meant to be running in it at a time.**
The point of `fleet-run` is switching your whole runtime environment to a
different work context, not stacking several tickets' windows side by side.
`start`-ing a ticket other than whatever's currently active stops everything
first; starting the same ticket that's already active just fills in whatever
windows are missing.

Each running app gets its own tmux window, named:

```
<repo>-<run-name>
```

e.g. `backend-api` — no ticket prefix, since only one ticket is ever live at
once, so nothing needs disambiguating by ticket.

`repos.yaml` is validated on load: repo names and run names must not contain
`:` (it separates `repo:run-name` on the command line), `tmux.session_name`
must not contain `.` or `:` (tmux silently rewrites both to `_` in a session
name, so the session could never be found again), and the derived window
names must be unique across the whole config — e.g. repo `admin` + run
`ui-dev` collides with repo `admin-ui` + run `dev` (both `admin-ui-dev`), and
two runs with the same name in one repo collide too. A violation is a clear
config error naming both entries.

Dots are fine in repo and run names (`my.app` + `dev` gives window
`my.app-dev`). tmux reads a `.` in a window target as a `window.pane`
separator, so `fleet-run` never targets an existing window by name: it looks
up the window's id (`@N`) with `list-windows`/`list-panes` and targets that
for kill, respawn, capture and select.

`tmux.config_file` (passed to `tmux -f`) only takes effect when `fleet-run`
itself starts the tmux server, i.e. when no server is running yet. If a tmux
server is already running (even one started outside fleet), the file is not
re-read; `source-file` it yourself.

### Tracking which ticket is active

Since window names don't carry the ticket, "which ticket is currently
running" is tracked via two global (`set-option -g`) tmux user
options (`activeticket.go`) — global rather than per-session because fleet only has the one
session, and `set-option -t` on a bare session target can't resolve without an attached client:

- `@fleet_task_ticket` — the active ticket's id
- `@fleet_task_description` — its description

`start` sets both right after the first window is created (tmux options need
a running server, and `set-option` doesn't start one); `stop` clears both once
it leaves the session with no windows running (if that also shut the tmux
server down, there's nothing left to clear and no warning is printed). If the
session is killed by hand while the tmux server survives (it has other
sessions), the record is left behind with nothing running: `status` flags it
as stale (`stale_active: true`) and any `stop` (bare, `--all`, or
`--ticket X`) clears it, reporting `cleared_active: true`. Until then
`fleet-task rm` refuses that ticket. `stop --ticket X` reads
`@fleet_task_ticket` purely as a safety assertion (it errors out, stopping
nothing, if `X` isn't the currently active ticket) rather than as a filter —
there's nothing else running to filter by.

These options are ordinary tmux user options, so your own tmux config can
read them directly in a status-line format string, e.g.:

```tmux
set -g status-right '#{@fleet_task_ticket}: #{@fleet_task_description}'
```

## Environment

- `FLEET_TMUX_SOCKET`: when non-empty, every tmux call gets `-L <value>`
  (before any other global flag such as `-f`), so fleet-run uses a private
  tmux server instead of your default one. `fleet-task` honours the same
  variable when checking the active ticket, so set it for both. Tests and
  experiments use it to stay off your real server.
- `FLEET_NO_INPUT=1`: never open a picker (see below).
- `FLEET_CONFIG_DIR`, `FLEET_REPOS_FILE`, `FLEET_STATE_DIR`: see
  [Config/state loading](#configstate-loading).

**Apps run in the tmux server's environment, not your shell's.** Every
window's command is run by the tmux *server* through a non-login
`$SHELL -c` (tmux's `default-shell`), with the environment the server was
started with (plus tmux's `update-environment` handling). It does not see
the environment of the shell that ran `fleet-run start`: e.g. switching Node
with `nvm use`, or any other `PATH`/env change made after the server
started, is not picked up. Restart the tmux server (or use the private one
via `FLEET_TMUX_SOCKET`), put the setup into the run `cmd` itself, or use a
`pre-run` hook for side effects.

## Hooks

Executables in `<config dir>/hooks/pre-run/` and `<config dir>/hooks/post-run/`
run in filename-sorted order (non-executable files and directories are
skipped): `pre-run` before each window is created or respawned (by `start`
or `restart`), `post-run` after it was created/respawned successfully. They
don't run for a window that is left alone because it is already running.
Each hook gets fleet-run's environment plus:

```
FLEET_TICKET=<ticket id>
FLEET_REPO=<repo name>
FLEET_WORKTREE_DIR=<worktree path from the task state>
FLEET_BRANCH=<branch from the task state>
FLEET_DESCRIPTION=<ticket description>
FLEET_RUN_NAME=<run name>
```

Hook stdout and stderr both go to fleet-run's stderr (stdout is reserved for
results such as `--json`). A failing hook only prints a warning; it never
blocks the launch (same policy as fleet-task's hooks). There is no timeout,
so a hook that hangs blocks `start`.

## Commands

Every subcommand has its own flag set and a `--help`. Flags may appear before
or after positional arguments (`fleet-run stop backend:api --json` works);
`--` ends flag parsing. Requested help (`--help`, `-h`, `help`, `<command>
--help`) goes to stdout and exits 0; usage printed because of an error goes
to stderr with exit 2.

`fleet-run --version` / `fleet-run version` prints `fleet-run <version>`.
The version is set at build time with `-ldflags "-X main.version=<v>"`;
an unstamped build prints `dev`, or `dev-<12-char commit>[-dirty]` when Go
embedded VCS info (a `go build` inside the git checkout).

**Non-interactive use (scripts, agents).** A picker is only ever opened when
stdin is a terminal and `FLEET_NO_INPUT` isn't `1` (`interactive.go`; only
stdin is checked, never stdout, so `$(...)` capture still works for humans).
Otherwise a command that would need a picker exits 2 naming the flag or
argument that avoids it, e.g. `pass repo:run-name ... or --all`. The guard
runs before any picker, since pickers open `/dev/tty` directly.

**Exit codes:** 0 ok; 1 the operation failed (fully or partly); 2 usage error
(bad flag/argument, unknown target, invalid ticket id, `--ticket` naming a
ticket with no state file) or input required.

**`--json`** prints one JSON document on stdout (also on exit 1, for `start`,
`restart` and `stop`); progress and warnings always go to stderr. Usage errors print no
JSON.

**Windows keep their output after the app exits.** Every window `fleet-run`
creates gets `remain-on-exit on` (chained into the same tmux invocation as
the window's creation, so even a command that exits immediately is caught).
A crashed app leaves a dead pane whose output `logs` can read and whose exit
status `status` reports; `start` respawns it.

### `fleet-run start [--ticket <id>] [--all] [repo:run-name ...] [--json]`

1. Refuses conflicting input up front: `--all` with explicit targets, or (no
   terminal) neither of them.
2. Loads and validates `repos.yaml` (including that `tmux.session_name` is
   set), then resolves which ticket to operate on:
   - `--ticket <id>` loads `tasks/<id>.json` directly (ids with path
     separators or `.`/`..` are rejected, exit 2).
   - With no `--ticket`: zero files under `tasks/*.json` is an error telling
     you to create one with `fleet-task new`; exactly one file is used
     automatically; two or more bring up a single-select picker over the
     ticket ids (exit 2 without a terminal).
3. Builds the available `repo:run-name` pairs from `repos.yaml`'s `run`
   entries, restricted to repos that have a worktree in the resolved task.
4. Picks targets: the positional `repo:run-name`s (duplicates ignored; an
   unknown one is a usage error before tmux is touched), every pair with
   `--all`, or otherwise the multiselect (`select.go`, a deliberate copy of
   `fleet-task`'s picker, kept in sync by hand). If nothing is selected,
   `start` stops here and touches nothing else.
5. Reads `@fleet_task_ticket`. If it's set to a *different* ticket, kills
   every window currently in the session first, aborting if any window can't
   be killed. If it matches, or nothing's active yet, nothing gets killed.
6. For each target:
   - no window yet: checks the worktree directory exists (a missing one fails
     that target instead of running the app in `$HOME`; for `runtime:
     windows` repos the WSL-side path is checked before translation), then
     creates the window. If the session doesn't exist yet it's created
     together with the first window (`tmux [-f <config_file>] new-session -d
     -s <session> -n <window> ...`; `-f` is a *global* tmux flag). Otherwise
     `tmux new-window -t =<session>:<index> -n <window> -c <worktree_dir>
     <cmd>`. For `runtime: windows` repos the worktree dir is translated with
     `wslpath -w` and the command runs as `powershell.exe -NoExit -Command
     "Set-Location -LiteralPath '<win-dir>'; <cmd>"` (the path is a
     PowerShell single-quoted literal, embedded `'` doubled). Either way
     `; set-option -w -t <target> remain-on-exit on` is chained on, where
     `<target>` is the creation target (`=<session>:<index>`, or
     `=<session>:` for the session's first window), never the window name.
   - window exists, pane alive: left alone (`already_running`).
   - window exists, pane dead: `tmux respawn-pane -k -t @<id>` with the
     target's current directory and command (`respawned`).

   `pre-run` hooks run before each create/respawn and `post-run` hooks after
   a successful one (see [Hooks](#hooks)).
7. Right after the first window is created or respawned (or immediately, if
   the session already had windows) sets
   `@fleet_task_ticket`/`@fleet_task_description`. If setting them fails,
   that target is reported `failed` and the next target retries.

All targets are attempted; exits 1 if any failed. `--json`:

```json
{"ticket": "PROJ-1", "switched_from": "", "ok": true,
 "results": [{"target": "backend:api", "window": "backend-api",
              "status": "started|already_running|respawned|failed",
              "error": "only when failed"}]}
```

`switched_from` is the previously active ticket whose windows were stopped,
or `""`.

### `fleet-run restart [--ticket <id>] [--all] [repo:run-name ...] [--json]`

Same as `start` (ticket resolution, target selection, ticket switching,
hooks, JSON shape), except that a target whose app is still running is
restarted with `respawn-pane -k` (status `restarted`) instead of being left
alone. Missing windows are created (`started`) and dead panes respawned
(`respawned`) as with `start`.

### `fleet-run stop [--ticket <id>] [--all] [repo:run-name ...] [--json]`

- `--ticket <id>`, if given, is a **safety assertion**, not a filter: errors
  out (exit 1, stopping nothing) unless `<id>` is the currently active ticket
  (`@fleet_task_ticket`). A stale record for `<id>` (see above) matches, so
  `stop --ticket <id>` clears it.
- With `--all`: kills every window currently running (dead panes included).
- With positional `repo:run-name` args: kills just those windows; named
  windows that aren't running are skipped with a warning. Malformed args are
  a usage error.
- `--all` together with positional args is a usage error.
- With neither: multiselects over the currently running windows (`repo:run-name`
  labels; windows whose repo is no longer in `repos.yaml` show their raw
  window name). Without a terminal: exit 2.
- Whenever a `stop` leaves the session with zero windows, it clears
  `@fleet_task_ticket`/`@fleet_task_description`. If there were no windows
  to begin with, any form of `stop` (even a bare one without a terminal)
  just clears a stale record and exits 0.
- Exits 1 if any window failed to be killed. `--json`:

```json
{"ok": true,
 "stopped": ["backend-api"], "stopped_targets": ["backend:api"],
 "not_running": [], "not_running_targets": [],
 "failed": [], "failed_targets": [],
 "cleared_active": false}
```

`stopped`/`not_running`/`failed` hold window names; each `*_targets` array is
parallel to its window-name array and holds the `repo:run-name` form, or `""`
for a window `repos.yaml` no longer describes. `cleared_active` is also true
when a stale record was cleared.

### `fleet-run status [--json]`

Read-only. Prints the active ticket and description and the windows running
in the fleet session; a window whose app exited is marked
`(dead, exit N; see fleet-run logs)`. An active ticket with no windows is
printed as `STALE` with the command that clears it. `--json`:

```json
{"session": "fleet", "active_ticket": "PROJ-1", "description": "...",
 "stale_active": false,
 "windows": ["backend-api", "web-dev"],
 "panes": [{"window": "backend-api", "target": "backend:api", "alive": true, "pid": 123},
           {"window": "web-dev", "target": "web:dev", "alive": false, "pid": 456, "exit_status": 7}]}
```

`windows` and `panes` are always arrays; `target` is `""` for a window
`repos.yaml` no longer describes; `exit_status` is present only for a dead
pane. If no tmux server or session exists it exits 0 and prints
`nothing running`.

### `fleet-run targets [--ticket <id>] [--json]`

Read-only. Lists the ticket's available `repo:run-name` targets (ticket
resolution as for `start`) as a table, or with `--json`:

```json
{"ticket": "PROJ-1",
 "targets": [{"target": "backend:api", "label": "backend:api", "window": "backend-api",
              "cwd": "/path/to/worktree", "cmd": "npm run start:dev",
              "runtime": "linux", "running": true}]}
```

`target` is the `repo:run-name` form, named as in every other document.
`label` is the same value, **deprecated**, kept for older consumers.

`running` is true only when this ticket is the active one and the window's
pane is alive. Window names don't encode the ticket, so another ticket's
same-named window never counts.

### `fleet-run logs <repo:run-name> [-n|--lines <N>] [--json]`

Read-only. Prints the last `N` lines (default 200, max 100000) of the
window's output including scrollback (`tmux capture-pane -p -J -S -N`; `-J`
joins wrapped lines). Trailing blank rows are dropped; for a dead pane the
blank rows tmux leaves between the output and its `Pane is dead (status N,
...)` line are dropped too, so they don't use up `N`. A target with no window
exits 1. `--json`: `{"target", "window", "alive", "lines": [...]}`.

### `fleet-run attach [repo:run-name]`

Attaches your terminal to the fleet session, first selecting the named
target's window if one is given. Inside a tmux client of the same server
(`$TMUX` names its socket) it runs `switch-client`; otherwise
`attach-session` (with `$TMUX` dropped from its environment, so attaching
from inside a *different* tmux server, e.g. with `FLEET_TMUX_SOCKET` set,
nests instead of failing). Needs a terminal on stdin: without one, or with
`FLEET_NO_INPUT=1`, exits 2. Exits 1 if nothing is running or the named
target has no window. No `--json`.

## Config/state loading

Same precedence as the rest of the fleet suite:

- Config dir: `$FLEET_CONFIG_DIR` else `$XDG_CONFIG_HOME/fleet` else
  `~/.config/fleet`. Repos file: `$FLEET_REPOS_FILE` else
  `<config dir>/repos.yaml`.
- State dir: `$FLEET_STATE_DIR` else `$XDG_STATE_HOME/fleet` else
  `~/.local/state/fleet`. Task files: `<state dir>/tasks/<ticket>.json`.

## Testing

`integration_test.go` drives the real command functions (`start`, `restart`,
`status`, `stop`, `logs`, `targets`, hooks, dead-pane respawn, dotted names,
ticket switch, stale-record clearing) against a **private** tmux server
(`FLEET_TMUX_SOCKET`, unique per test, killed in cleanup) with temp config
and state dirs and an empty tmux config; they never touch your own tmux
server or `~/.config`/`~/.local/state`. They're skipped when `tmux` isn't
installed.

Everything else that doesn't require a live `tmux`/`powershell.exe` or an
interactive terminal is covered by unit tests (`go test ./...`):

- `config_test.go` — `repos.yaml` parsing, including `tmux`, `worktree_root`,
  and per-repo `run`/`runtime`.
- `task_test.go` — `tasks/<ticket>.json` parsing (`created_at` is a
  `time.Time`, same as fleet-task writes) and the `--ticket`
  resolution rules (explicit / single-task-fallback / zero-or-many error).
  The zero-or-many case still errors at this layer; the bubbletea picker for
  the many-tasks case is wired in one level up, in `cmd_start.go`'s
  `loadStartContext`, so it stays out of these pure-logic tests. Also
  ticket-id validation.
- `window_test.go` — window name construction (`<repo>-<run-name>`).
- `config_test.go` also covers rejecting `:` in names and window-name
  collisions.
- `active_logic_test.go` — the switch/clear-active decisions
  (`needsSwitch`, `shouldClearActive`).
- `cmdutil_test.go` — flags-after-positionals parsing.
- `cmd_status_test.go` — `status` text/JSON rendering, dead-pane marking.
- `cmd_targets_test.go` — target rows and the active-ticket rule for
  `running`.
- `cmd_logs_test.go` — trimming captured output, dead-pane padding.
- `errors_test.go` — exit-code mapping (usage/input-required → 2).
- `interactive_test.go` — the terminal / `FLEET_NO_INPUT` decision.
- `panes_test.go` — parsing `list-panes` output into pane state.
- `start_logic_test.go` — explicit/`--all`/picker target resolution and its
  usage errors, existing-window decisions (`already_running` vs
  `respawned`).
- `pairs_test.go` — deriving available `repo:run-name` pairs from a fixture
  `repos.yaml` + task state, including skipping repos absent from
  `repos.yaml`.
- `stop_logic_test.go` — the `--ticket`-matches-active safety assertion,
  positional `repo:run-name` parsing, picker label/window-name mapping.
- `hooks_test.go` — hook discovery/ordering, environment, failing hooks.
- `version_test.go` — version string from `-ldflags` / VCS build info.
- `cmd_attach_test.go` — `$TMUX` same-server detection, terminal requirement.
- `tmux_cmd_test.go` — the pure argv-building functions for every `tmux`
  invocation shape (`new-session` with/without `-f`, `new-window` for both
  linux and windows runtimes with the chained `remain-on-exit`,
  `respawn-pane`, `list-panes`, `capture-pane`, `kill-window`,
  `list-windows`, `select-window`/`switch-client`/`attach-session`, the
  `-L` socket prefix, PowerShell path quoting, and the
  `set-option`/`show-options` calls used for the active-ticket record).

`tmux_exec_test.go` covers the worktree-path helpers in `tmux_exec.go`
(symlink resolution for windows-runtime worktrees, the missing-worktree
check) and `list-windows` parsing. The rest of the actual subprocess execution and the interactive
bubbletea picker (`select.go`) are thin, deliberately untested wrappers — they
have no branching logic of their own left to test once the argv-building and
selection logic is verified.
