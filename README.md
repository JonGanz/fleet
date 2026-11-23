# fleet

Run several tickets in parallel, each in its own set of git worktrees and its own set of
running apps, without their files or processes stepping on each other.

A "ticket" is just an id you choose: a Jira key (`PROJ-123`), a GitHub issue (`gh-123`), a Linear
or GitLab id, or a plain name (`fix-login`). fleet never talks to an issue tracker; the id only
names the task's state file, worktree directory and (via `branch_template`) its branches.

## Disclaimer

This repository's contents — code, tests, and documentation — are 100% LLM-produced.

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
  across worktrees whose `package-lock.json` and `.npmrc` are identical (via hardlinks: several directory
  entries pointing at the same file on disk, so extra copies cost no space or install time).

They are three Go programs that coordinate only through files on disk (a config you write, state
files they own) and by running each other by name — no daemon. They're built from one Go module
that shares the code implementing those files and conventions. The authoritative description of
them is [`docs/CONTRACT.md`](docs/CONTRACT.md).

## Quick start

1. **Install** (needs the tools in "Requirements" below):

   ```sh
   ./install.sh                                  # builds into ~/.local/bin (override with BIN_DIR=...)
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

   This is the minimum. Other options (`defaults`, `branch_template`, `worktree_root`,
   `hooks.timeout`, `git.network_timeout`, the `runtime: windows` settings, ...) are documented
   in [`docs/CONTRACT.md`](docs/CONTRACT.md), which is the full reference for the config.

3. **Work a ticket:**

   ```sh
   fleet-task new                        # prompts for ticket + description, pick repos/patches -> worktrees ready
   fleet-run start --ticket PROJ-1234    # pick which apps to bring up in tmux
   cd "$(fleet-task jump)"               # pick a worktree and cd into it
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

## Install

`./install.sh` is a POSIX `sh` script (`set -eu`; `--help` for usage). It checks that `go` is on
`PATH`, builds `fleet-task`, `fleet-cache` and `fleet-run` with `go build -trimpath`, stamps each
with a version (`$FLEET_VERSION`, else `git describe --tags --always --dirty`, else `dev`; shown by
`<tool> version`), and moves each into `$BIN_DIR` (default `~/.local/bin`, created if missing) only
once its build succeeded. `make install` does the same. Make sure that directory is on your `$PATH`: `fleet-task new` runs `fleet-cache` by name,
so all three must be reachable.

It also installs the Claude Code skill: a copy of `skills/fleet/` goes to
`$XDG_DATA_HOME/fleet/skills/fleet` (default `~/.local/share/fleet/...`), and
`$CLAUDE_CONFIG_DIR/skills/fleet` (default `~/.claude/skills/fleet`) is symlinked to that copy. The
skill therefore matches the installed binaries rather than whatever the checkout holds; rerun
`./install.sh` after pulling. An existing non-symlink at the link path is left alone.
`./install.sh --uninstall` removes the binaries, the skill copy and the link (only if it points at
fleet's skill).

A subprocess can't change its parent shell's directory, so `fleet-task jump` only prints a path:
use it as `cd "$(fleet-task jump)"`.

## The commands

Each tool's README has the full reference (flags, exit codes, behaviour).

- **`fleet-task`** — [`cmd/fleet-task/README.md`](cmd/fleet-task/README.md)
  - `new [ticket]` — create a ticket's worktrees (prompts, or `--repo`/`-d`/`--skip-patch` flags;
    runs hooks, a failing `pre-create` hook skips that repo; installs `node_modules` via
    `fleet-cache`); a worktree left by an interrupted run on the right branch is adopted
  - `list [--status]` / `show <ticket>` — list tickets (`--status`: count repos with uncommitted
    changes) / show one ticket's repos, branches, paths, patches, uncommitted changes (not counting
    fleet's own patches) and unpushed commits
  - `repos` / `patches [repo]` — configured repos and run targets / available patches with summaries
  - `jump [ticket] [--repo <name>]` — print a worktree path (`cd "$(fleet-task jump)"`)
  - `edit [ticket]` — add or remove repos (pickers, or `--add`/`--remove`); removing a repo with
    uncommitted changes needs confirmation or `--force`
  - `mv [old] [new]` — rename a ticket, moving its worktrees
  - `rm [ticket]` — remove a ticket's worktrees and state (picker if no ticket given); refuses
    worktrees with uncommitted changes (asks on a terminal) unless `--force`; deletes each local
    branch whose commits are all on a remote and keeps the rest (`--keep-branch` /
    `--delete-branch` override)
  - `unlock <ticket>` — remove a ticket's lock left by a killed `fleet-task` (refuses while its
    owner is still running)
- **`fleet-run`** — [`cmd/fleet-run/README.md`](cmd/fleet-run/README.md)
  - `start [--ticket <id>] [--all] [repo:run-name ...]` — start a ticket's apps, one tmux window
    each, named `<repo>-<run-name>` (e.g. `backend-api`); switches tickets if another is active
  - `restart [--ticket <id>] [--all] [repo:run-name ...]` — like `start`, but also restarts apps
    that are still running
  - `stop [--ticket <id>] [--all] [repo:run-name ...]` — stop windows (also clears a stale
    active-ticket record left by a session killed outside fleet-run)
  - `status` — show the active ticket and windows, including crashed (dead) ones
  - `targets [--ticket <id>]` — list a ticket's runnable `repo:run-name` targets
  - `logs <repo:run-name> [-n N]` — print an app's recent output, even after it crashed
  - `attach [repo:run-name]` — attach your terminal to the fleet tmux session (humans only)
- **`fleet-cache`** — [`cmd/fleet-cache/README.md`](cmd/fleet-cache/README.md)
  - `ensure <dir>` — link `<dir>/node_modules` from the shared cache (run for you by `fleet-task new`)
  - `gc` — report, or with `--roots` and `--force` delete, cache entries no longer referenced
  - `list` — show cache entries and staging dirs with their size
  - `ensure-windows` / `gc-windows` — the same for `runtime: windows` repos, using a Windows-native cache

Each tool also has `version` (or `--version`) and `--help`.

For scripts and AI agents, every fleet-task/fleet-run command except `fleet-task jump`,
`fleet-run attach` and `version` takes `--json` (as do `fleet-cache gc`/`gc-windows`/`list`), all
three tools exit 0 ok / 1 failed / 2 usage error or missing input and print errors and warnings on
stderr as `<tool>: error: ...` / `<tool>: warning: ...`, nothing prompts without a
terminal (or with `FLEET_NO_INPUT=1`; a missing value exits 2), and
[`skills/fleet/SKILL.md`](skills/fleet/SKILL.md) is a ready-made Claude Code skill (installed by
`install.sh`; see [Install](#install)).

Config, state and cache live under XDG directories (`~/.config/fleet`, `~/.local/state/fleet`,
`~/.cache/fleet`, or under `$XDG_CONFIG_HOME` etc. when those are absolute paths; a relative one is
ignored with a warning), overridable with `FLEET_CONFIG_DIR`, `FLEET_REPOS_FILE`, `FLEET_STATE_DIR`
and `FLEET_CACHE_DIR` (relative values are taken relative to the current directory). Optional hook scripts and patch files live in the config directory; see
`docs/CONTRACT.md`. `FLEET_TMUX_SOCKET=<name>` makes fleet use a private tmux server
(`tmux -L <name>`).

## Development

One Go module (`github.com/JonGanz/fleet`): the three binaries are under `cmd/<tool>/`, the code
they share under `internal/` (paths, `repos.yaml`, state files, hooks, the picker, CLI plumbing,
WSL helpers). From the repo root:

```sh
go build ./... && go vet ./... && go test ./...
```

The root `Makefile` wraps these: `make check` (gofmt check, vet, `go test -race`),
`make build` (version-stamped binaries in `dist/`), `make lint` (staticcheck, if installed),
`make install`, `make clean`. CI (`.github/workflows/ci.yml`) runs the same plus
staticcheck, govulncheck and shellcheck on `install.sh`. fleet-run's integration tests drive a
private tmux server and fleet-task's git tests use real temp repos; they're skipped when
`tmux`/`git` is missing.
