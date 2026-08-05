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

### Tracking which ticket is active

Since window names don't carry the ticket, "which ticket is currently
running" is tracked via two session-scoped tmux user options
(`activeticket.go`):

- `@fleet_task_ticket` — the active ticket's id
- `@fleet_task_description` — its description

`start` sets both on every successful run; `stop` clears both once it leaves
the session with no windows running. `stop --ticket X` reads
`@fleet_task_ticket` purely as a safety assertion (it errors out, stopping
nothing, if `X` isn't the currently active ticket) rather than as a filter —
there's nothing else running to filter by.

These options are ordinary tmux user options, so your own tmux config can
read them directly in a status-line format string, e.g.:

```tmux
set -g status-right '#{@fleet_task_ticket}: #{@fleet_task_description}'
```

## Commands

### `fleet-run start [--ticket <id>]`

1. Resolves which ticket to operate on:
   - `--ticket <id>` loads `tasks/<id>.json` directly.
   - With no `--ticket`, there's no "plain checkout" path outside of
     worktrees per the contract, so `fleet-run` requires there to be
     **exactly one** file under `tasks/*.json`; zero or more than one is an
     error telling you to pass `--ticket` explicitly.
2. Builds the list of available `repo:run-name` pairs from `repos.yaml`'s
   `run` entries, restricted to repos that actually have a worktree in the
   resolved task.
3. Multiselects from that list via `fzf --multi` (candidates piped in on
   stdin, `stderr` inherited for the interactive UI, `stdout` captured for
   the selection — fzf opens `/dev/tty` itself for keyboard/screen handling
   regardless of stdout redirection, so no `/dev/tty`-opening fallback was
   needed). If nothing is selected, `start` stops here and touches nothing
   else — no session/option changes — so backing out never tears down a
   working environment.
4. Ensures the tmux session exists (`tmux has-session`; if absent,
   `tmux [-f <config_file>] new-session -d -s <session>` — note `-f` is a
   *global* tmux flag and must precede the subcommand, not follow it).
5. Reads `@fleet_task_ticket`. If it's set to a *different* ticket than the
   one just resolved, kills every window currently in the session first
   (this is what used to be a separate `switch` command). If it matches, or
   nothing's active yet, nothing gets killed.
6. Sets `@fleet_task_ticket`/`@fleet_task_description` to the resolved
   ticket.
7. For each selection, computes the window name and creates it with
   `tmux new-window -t <session> -n <window> -c <worktree_dir> <cmd>` — or,
   for `runtime: windows` repos, translates the worktree dir with
   `wslpath -w` and instead runs
   `powershell.exe -NoExit -Command "cd '<win-dir>'; <cmd>"` (tmux's own
   `-c` only understands WSL-side paths, so it can't be used to seed a
   Windows process's working directory).
8. Skips (with a warning) any selection whose window name already exists,
   rather than creating a duplicate.

### `fleet-run stop [--ticket <id>] [--all] [repo:run-name ...]`

- `--ticket <id>`, if given, is a **safety assertion**, not a filter: errors
  out (stopping nothing) unless `<id>` is the currently active ticket
  (`@fleet_task_ticket`). Since only one ticket's windows are ever running,
  there's nothing else to filter by.
- With `--all`: kills every window currently running.
- With positional `repo:run-name` args: kills just those specific windows.
- With neither: multiselects (fzf) over the currently running windows.
- Whenever a `stop` leaves the session with zero windows running, it clears
  `@fleet_task_ticket`/`@fleet_task_description`.

## Config/state loading

Same precedence as the rest of the fleet suite:

- Config dir: `$FLEET_CONFIG_DIR` else `$XDG_CONFIG_HOME/fleet` else
  `~/.config/fleet`. Repos file: `$FLEET_REPOS_FILE` else
  `<config dir>/repos.yaml`.
- State dir: `$FLEET_STATE_DIR` else `$XDG_STATE_HOME/fleet` else
  `~/.local/state/fleet`. Task files: `<state dir>/tasks/<ticket>.json`.

## Testing

Everything that doesn't require a live `tmux`/`fzf`/`powershell.exe` is
covered by unit tests (`go test ./...`):

- `config_test.go` — `repos.yaml` parsing, including `tmux`, `worktree_root`,
  and per-repo `run`/`runtime`.
- `task_test.go` — `tasks/<ticket>.json` parsing and the `--ticket`
  resolution rules (explicit / single-task-fallback / zero-or-many error).
- `window_test.go` — window name construction (`<repo>-<run-name>`).
- `pairs_test.go` — deriving available `repo:run-name` pairs from a fixture
  `repos.yaml` + task state, including skipping repos absent from
  `repos.yaml`.
- `stop_logic_test.go` — the `--ticket`-matches-active safety assertion,
  positional `repo:run-name` parsing.
- `tmux_cmd_test.go` — the pure argv-building functions for every `tmux`
  invocation shape (`new-session` with/without `-f`, `new-window` for both
  linux and windows runtimes, `kill-window`, `list-windows`, and the
  `set-option`/`show-options` calls used for the active-ticket record).

Actual subprocess execution (`tmux_exec.go`, `fzf.go`) is a thin, deliberately
untested wrapper around these pure functions — it has no branching logic of
its own left to test once the argv-building and selection logic is verified.
