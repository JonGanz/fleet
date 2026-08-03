# fleet-run

`fleet-run` manages the tmux windows that run each task's applications, per
the shared contract in [`../docs/CONTRACT.md`](../docs/CONTRACT.md). It is a
standalone Go module (module path `fleet-run`) with no dependency on
`fleet-task` or `fleet-cache` Go code — it only reads the on-disk files those
tools produce (`repos.yaml`, `tasks/<ticket>.json`).

## The fixed-session / one-window-per-app model

There is exactly **one** tmux session for the whole fleet, named by
`repos.yaml`'s `tmux.session_name` (e.g. `fleet`). It is created once, on
first use, and is **never** renamed or recreated per ticket — every ticket's
windows live in that same session, side by side.

Each running app gets its own tmux window, named:

```
<ticket>-<repo>-<run-name>
```

e.g. `PROJ-1234-backend-api`. All window lifecycle operations (`start`,
`stop`, `switch`) key off this naming pattern — never off session identity,
since there is only ever one session.

### Window-name parsing ambiguity

Hyphens are both the field separator and a character commonly present
*inside* ticket ids (`PROJ-1234`), repo names, and run names, so a bare
window-name string does not uniquely decompose on its own. `parseWindowName`
(in `window.go`) resolves this by taking the caller-supplied set of known
ticket ids and repo names currently in play, and doing a longest-prefix match
first for the ticket, then for the repo; whatever's left is the run name
verbatim. This means a window belonging to a since-deleted ticket or a repo
no longer in `repos.yaml` cannot be parsed back precisely. Filtering "does
this window belong to ticket X", which is all `stop`/`switch` actually need,
uses the unambiguous `hasWindowPrefix`/`filterWindowsByTicketPrefix` helpers
instead and never needs full decomposition.

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
   needed).
4. Ensures the tmux session exists (`tmux has-session`; if absent,
   `tmux [-f <config_file>] new-session -d -s <session>` — note `-f` is a
   *global* tmux flag and must precede the subcommand, not follow it).
5. For each selection, computes the window name and creates it with
   `tmux new-window -t <session> -n <window> -c <worktree_dir> <cmd>` — or,
   for `runtime: windows` repos, translates the worktree dir with
   `wslpath -w` and instead runs
   `powershell.exe -NoExit -Command "cd '<win-dir>'; <cmd>"` (tmux's own
   `-c` only understands WSL-side paths, so it can't be used to seed a
   Windows process's working directory).
6. Skips (with a warning) any selection whose window name already exists,
   rather than creating a duplicate.

### `fleet-run stop [--ticket <id>] [--all [--everything]] [repo:run-name ...]`

- With `--all` and `--ticket`: kills every window with that ticket's prefix.
- With `--all` and no `--ticket`: **errors**, asking you to either add
  `--ticket` or confirm with `--everything` — a bare `stop --all` can't
  accidentally nuke every ticket's windows.
- With positional `repo:run-name` args: kills just those windows for the
  resolved ticket (same `--ticket`-or-single-task-fallback resolution as
  `start`).
- With neither: multiselects (fzf) over the currently running windows,
  filtered to `--ticket`'s prefix (or all windows, with a warning, if no
  ticket given).

### `fleet-run switch --to <ticket>`

Equivalent to stopping *every* window currently running in the session
(only one ticket's app set is meant to be live at a time — see GOAL.md),
then running the same selection+start flow as `start --ticket <to>`. This is
implemented as a direct in-process call to the internal
`killAllWindowsInSession` + `startFlow` functions, not by shelling out to its
own binary.

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
- `window_test.go` — window name construction, the reverse parse (including
  the hyphen-ambiguity cases above), and ticket-prefix filtering.
- `pairs_test.go` — deriving available `repo:run-name` pairs from a fixture
  `repos.yaml` + task state, including skipping repos absent from
  `repos.yaml`.
- `stop_logic_test.go` — the `--all`/`--everything` safety check, positional
  `repo:run-name` parsing, and the windows-to-kill filtering.
- `tmux_cmd_test.go` — the pure argv-building functions for every `tmux`
  invocation shape (`new-session` with/without `-f`, `new-window` for both
  linux and windows runtimes, `kill-window`, `list-windows`).

Actual subprocess execution (`tmux_exec.go`, `fzf.go`) is a thin, deliberately
untested wrapper around these pure functions — it has no branching logic of
its own left to test once the argv-building and selection logic is verified.
