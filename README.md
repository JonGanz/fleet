# fleet

Run several Jira tickets in parallel, each in its own set of git worktrees and its own set of
running apps, without their files or processes stepping on each other.

## The problem

A ticket often touches a few repos out of a larger suite (backend, database, several web UIs).
Working it means checking out a branch in each, running the relevant apps to test, then pushing and
moving the ticket along. A single checkout per repo can only be on one branch and in one state at a
time, so working two tickets at once — or letting AI agents work several — means constant
stashing, switching and port clashes.

`fleet` gives each ticket its own directories and its own running apps:

- **`fleet-task`** creates, per ticket, a *git worktree* in each repo the ticket touches (an
  extra working directory on its own branch, sharing one git history with the others).
- **`fleet-run`** starts that ticket's apps in tmux windows, and switches the whole running set to
  another ticket when you ask.
- **`fleet-cache`** makes the worktrees ready to use immediately by sharing one `npm ci` result
  across worktrees whose `package-lock.json` is identical (via hardlinks: several directory
  entries pointing at the same file on disk, so extra copies cost no space or install time).

They are three independent Go programs that coordinate only through files on disk (a config you
write, state files they own) and by running each other by name — no shared code, no daemon. The
authoritative description of those files and conventions is [`docs/CONTRACT.md`](docs/CONTRACT.md).

## Quick start

1. **Install** (needs the tools in "Requirements" below):

   ```sh
   ./install.sh                                  # builds into ~/.local/bin (override with BIN_DIR=...)
   source /path/to/fleet/fleet-task/contrib/fleet.sh   # from your shell profile; defines `fj`
   ```

2. **Write a minimal `~/.config/fleet/repos.yaml`:**

   ```yaml
   tmux:
     session_name: fleet

   repos:
     - name: backend
       origin: git@github.com:org/backend.git
       base: ~/dev/.fleet-base/backend      # shared bare clone, created on first use
       default_branch: main
       runtime: linux
       run:
         - name: api
           cmd: "npm run start:dev"
   ```

   This is the minimum. Other options (`defaults`, `branch_template`, `worktree_root`, the
   `runtime: windows` settings, ...) are documented in [`docs/CONTRACT.md`](docs/CONTRACT.md),
   which is the full reference for the config.

3. **Work a ticket:**

   ```sh
   fleet-task new                        # prompts for ticket + description, pick repos/patches -> worktrees ready
   fleet-run start --ticket PROJ-1234    # pick which apps to bring up in tmux
   fj                                    # pick a worktree and cd into it (the shell helper from step 1)
   fleet-run status                      # what's running, for which ticket
   fleet-run stop --all                  # stop the apps
   fleet-task rm PROJ-1234               # done: remove the worktrees and the ticket's state
   ```

   Running `fleet-run start --ticket PROJ-5678` while PROJ-1234's apps are up stops them first and
   starts PROJ-5678's: only one ticket's apps run at a time.

## Requirements

- Go 1.25.4+ to build.
- `git` 2.5+ (worktrees), `tmux`, `npm`.
- WSL2 only if you need `runtime: windows` repos (apps and git that must run natively on
  Windows; the tools shell out to `powershell.exe`).

Pickers (repo/patch/ticket selection) are built in; no `fzf` or other external picker is needed.

## Install

`./install.sh` is a POSIX `sh` script (`set -eu`). It builds `fleet-task`, `fleet-cache` and
`fleet-run` with `go build` and writes them into `$BIN_DIR` (default `~/.local/bin`, created if
missing). Make sure that directory is on your `$PATH`: `fleet-task new` runs `fleet-cache` by name,
so all three must be reachable.

A subprocess can't change its parent shell's directory, so `fleet-task jump` only prints a path.
Source `fleet-task/contrib/fleet.sh` from your shell profile to get `fj`, which wraps it and does the
`cd`.

## The commands

Each tool's README has the full reference (flags, exit codes, behaviour).

- **`fleet-task`** — [`fleet-task/README.md`](fleet-task/README.md)
  - `new` — create a ticket's worktrees (prompts for ticket/description, repos, patches; runs hooks;
    installs `node_modules` via `fleet-cache`)
  - `list [--json]` — list tickets
  - `jump [ticket]` — pick a worktree and print its path (use via `fj`)
  - `edit [ticket]` — add or remove repos of an existing ticket
  - `mv [old] [new]` — rename a ticket, moving its worktrees
  - `rm [ticket]` — remove a ticket's worktrees and state (picker if no ticket given)
- **`fleet-run`** — [`fleet-run/README.md`](fleet-run/README.md)
  - `start [--ticket <id>]` — start a ticket's apps, one tmux window each, named
    `<repo>-<run-name>` (e.g. `backend-api`); switches tickets if another is active
  - `stop [--ticket <id>] [--all] [repo:run-name ...]` — stop windows
  - `status [--json]` — show the active ticket and running windows
- **`fleet-cache`** — [`fleet-cache/README.md`](fleet-cache/README.md)
  - `ensure <dir>` — link `<dir>/node_modules` from the shared cache (run for you by `fleet-task new`)
  - `gc` — report, or with `--roots` and `--force` delete, cache entries no longer referenced
  - `ensure-windows` / `gc-windows` — the same for `runtime: windows` repos, using a Windows-native cache

Config, state and cache live under XDG directories (`~/.config/fleet`, `~/.local/state/fleet`,
`~/.cache/fleet`), overridable with `FLEET_CONFIG_DIR`, `FLEET_REPOS_FILE`, `FLEET_STATE_DIR` and
`FLEET_CACHE_DIR`. Optional hook scripts and patch files live in the config directory; see
`docs/CONTRACT.md`.

## Development

Each CLI is an independent Go module:

```sh
cd fleet-task && go build ./... && go vet ./... && go test ./...
cd fleet-run   && go build ./... && go vet ./... && go test ./...
cd fleet-cache && go build ./... && go vet ./... && go test ./...
```

Shell glue test for `fj` (requires [bats-core](https://github.com/bats-core/bats-core)):

```sh
cd fleet-task/contrib && bats fleet.bats
```

## Disclaimer

This repository's contents — code, tests, and documentation — are 100% LLM-produced.
