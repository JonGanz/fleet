# fleet-run

`fleet-run` manages the tmux windows that run each task's applications, per
the shared contract in [`../docs/CONTRACT.md`](../docs/CONTRACT.md). It is a
standalone Go module (module path `fleet-run`) with no dependency on
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
`:` (it separates `repo:run-name` on the command line), and the derived window
names must be unique across the whole config — e.g. repo `admin` + run
`ui-dev` collides with repo `admin-ui` + run `dev` (both `admin-ui-dev`), and
two runs with the same name in one repo collide too. A violation is a clear
config error naming both entries.

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
server down, there's nothing left to clear and no warning is printed). `stop --ticket X` reads
`@fleet_task_ticket` purely as a safety assertion (it errors out, stopping
nothing, if `X` isn't the currently active ticket) rather than as a filter —
there's nothing else running to filter by.

These options are ordinary tmux user options, so your own tmux config can
read them directly in a status-line format string, e.g.:

```tmux
set -g status-right '#{@fleet_task_ticket}: #{@fleet_task_description}'
```

## Commands

Every subcommand has its own flag set and a `--help`. Flags may appear before
or after positional arguments (`fleet-run stop backend:api --all` works);
`--` ends flag parsing. Errors, including usage errors, exit 1.

### `fleet-run start [--ticket <id>]`

1. Loads and validates `repos.yaml` (including that `tmux.session_name` is
   set, before anything interactive happens), then resolves which ticket to
   operate on:
   - `--ticket <id>` loads `tasks/<id>.json` directly.
   - With no `--ticket`, there's no "plain checkout" path outside of
     worktrees per the contract. Zero files under `tasks/*.json` is still an
     error telling you to create one with `fleet-task new` first; exactly one
     file is used automatically; two or more files bring up an in-process
     bubbletea single-select picker (vim movement, `/` to filter) over the
     ticket ids so you don't have to pass `--ticket` by hand.
2. Builds the list of available `repo:run-name` pairs from `repos.yaml`'s
   `run` entries, restricted to repos that actually have a worktree in the
   resolved task.
3. Multiselects from that list via the same in-process bubbletea picker
   (`select.go`, a deliberate copy of `fleet-task`'s picker, kept in sync by hand),
   opened directly against `/dev/tty` so it works regardless of stdout
   redirection. If nothing is selected, `start` stops here and touches nothing
   else — no session/option changes — so backing out never tears down a
   working environment.
4. Reads `@fleet_task_ticket`. If it's set to a *different* ticket than the
   one just resolved, kills every window currently in the session first
   (this is what used to be a separate `switch` command), aborting if any
   window can't be killed. If it matches, or nothing's active yet, nothing
   gets killed.
5. For each selection, checks the worktree directory exists (a missing one —
   e.g. a stale task state file — fails that target with a clear error
   instead of running the app in `$HOME`; for `runtime: windows` repos the
   WSL-side path is checked before translation), then creates the window. If
   the session doesn't exist yet it's created together with the first window
   (`tmux [-f <config_file>] new-session -d -s <session> -n <window> ...`;
   `-f` is a *global* tmux flag and must precede the subcommand). Otherwise
   `tmux new-window -t =<session>:<index> -n <window> -c <worktree_dir> <cmd>`.
   For `runtime: windows` repos the worktree dir is translated with
   `wslpath -w` and the command runs as
   `powershell.exe -NoExit -Command "cd '<win-dir>'; <cmd>"` (tmux's own
   `-c` only understands WSL-side paths).
6. Right after the first window is created (or immediately, if the session
   already had windows) sets `@fleet_task_ticket`/`@fleet_task_description`.
7. Skips (with a warning) any selection whose window name already exists,
   rather than creating a duplicate.

All selected targets are attempted. **Exit code:** 0 if every selected target
started (or already existed); 1 if any failed to start (a summary error is
printed after the per-target warnings).

### `fleet-run stop [--ticket <id>] [--all] [repo:run-name ...]`

- `--ticket <id>`, if given, is a **safety assertion**, not a filter: errors
  out (stopping nothing) unless `<id>` is the currently active ticket
  (`@fleet_task_ticket`). Since only one ticket's windows are ever running,
  there's nothing else to filter by.
- With `--all`: kills every window currently running.
- With positional `repo:run-name` args: kills just those specific windows.
- With neither: multiselects (bubbletea) over the currently running windows.
  The picker shows the same `repo:run-name` form the positional args take
  (window names themselves stay `repo-run-name`; windows whose repo is no
  longer in `repos.yaml` show their raw window name).
- Whenever a `stop` leaves the session with zero windows running, it clears
  `@fleet_task_ticket`/`@fleet_task_description`.
- Exits 1 if any window failed to be killed.

### `fleet-run status [--json]`

Read-only. Prints the active ticket and description and the windows running
in the fleet session. Plain text by default; `--json` prints
`{"session", "active_ticket", "description", "windows": [...]}` (`windows` is
always an array). If no tmux server or session exists it exits 0 and prints
`nothing running` (JSON: empty ticket and empty `windows`).

## Config/state loading

Same precedence as the rest of the fleet suite:

- Config dir: `$FLEET_CONFIG_DIR` else `$XDG_CONFIG_HOME/fleet` else
  `~/.config/fleet`. Repos file: `$FLEET_REPOS_FILE` else
  `<config dir>/repos.yaml`.
- State dir: `$FLEET_STATE_DIR` else `$XDG_STATE_HOME/fleet` else
  `~/.local/state/fleet`. Task files: `<state dir>/tasks/<ticket>.json`.

## Testing

Everything that doesn't require a live `tmux`/`powershell.exe` or an
interactive terminal is covered by unit tests (`go test ./...`):

- `config_test.go` — `repos.yaml` parsing, including `tmux`, `worktree_root`,
  and per-repo `run`/`runtime`.
- `task_test.go` — `tasks/<ticket>.json` parsing (`created_at` is a
  `time.Time`, same as fleet-task writes) and the `--ticket`
  resolution rules (explicit / single-task-fallback / zero-or-many error).
  The zero-or-many case still errors at this layer; the bubbletea picker for
  the many-tasks case is wired in one level up, in `cmd_start.go`'s
  `loadStartContext`, so it stays out of these pure-logic tests.
- `window_test.go` — window name construction (`<repo>-<run-name>`).
- `config_test.go` also covers rejecting `:` in names and window-name
  collisions.
- `active_logic_test.go` — the switch/clear-active decisions
  (`needsSwitch`, `shouldClearActive`).
- `cmdutil_test.go` — flags-after-positionals parsing.
- `cmd_status_test.go` — `status` text/JSON rendering.
- `pairs_test.go` — deriving available `repo:run-name` pairs from a fixture
  `repos.yaml` + task state, including skipping repos absent from
  `repos.yaml`.
- `stop_logic_test.go` — the `--ticket`-matches-active safety assertion,
  positional `repo:run-name` parsing, picker label/window-name mapping.
- `tmux_cmd_test.go` — the pure argv-building functions for every `tmux`
  invocation shape (`new-session` with/without `-f`, `new-window` for both
  linux and windows runtimes, `kill-window`, `list-windows`, and the
  `set-option`/`show-options` calls used for the active-ticket record).

`tmux_exec_test.go` covers only the worktree-path helpers in `tmux_exec.go`
(symlink resolution for windows-runtime worktrees, the missing-worktree
check). The rest of the actual subprocess execution and the interactive
bubbletea picker (`select.go`) are thin, deliberately untested wrappers — they
have no branching logic of their own left to test once the argv-building and
selection logic is verified.
