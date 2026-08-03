# fleet

Three small CLIs for running several Jira tickets in parallel — each in its own set of git
worktrees, each with its own subset of running applications — without their working trees or
running processes stepping on each other. Useful once you're driving more than one AI-agent
pipeline at a time and a single shared checkout per repo can't keep up.

They coordinate purely through files on disk (a config you write, state files they own) and by
shelling out to each other by name — no shared Go code, no daemon. See `docs/CONTRACT.md` for the
full shared contract (paths, `repos.yaml` schema, state file format, hook/tmux conventions).

## Requirements

- Go 1.22+ to build.
- `git` 2.5+ (worktrees), `tmux`, `fzf`, `npm`.
- WSL2 only if you need the `runtime: windows` path (shells out to `powershell.exe`).

## Pieces

- **`fleet-task`** — turns a ticket into a set of git worktrees. `fleet-task new` prompts for a
  ticket/description, lets you fzf-multiselect which repos and which patches to apply, then for
  each repo: clones/fetches a shared bare repo, adds a worktree on a branch named after the
  ticket, applies patches, runs hooks, and populates `node_modules` via `fleet-cache`.
  `fleet-task list`/`jump`/`rm` list, fzf-jump to, and tear down worktrees. See
  [`fleet-task/README.md`](fleet-task/README.md).

- **`fleet-run`** — runs each task's applications in tmux. One fixed, configurable session for
  the whole fleet (never renamed per ticket); each running app is its own window named
  `<ticket>-<repo>-<run-name>`. `start`/`stop`/`switch` let you fzf-multiselect which apps to
  bring up, tear down, or swap for another ticket's set. See
  [`fleet-run/README.md`](fleet-run/README.md).

- **`fleet-cache`** — a `node_modules` hardlink cache keyed by the hash of `package-lock.json`, so
  N worktrees of the same repo/lockfile share one `npm ci` instead of paying for it N times.
  `fleet-task` calls this automatically; `fleet-cache gc` reclaims stale entries. See
  [`fleet-cache/README.md`](fleet-cache/README.md).

## Gluing it together

All three read the same `repos.yaml` and the same `tasks/*.json` state files (one file per
ticket, never a single shared one — so parallel tickets never contend on the same file). Start
with a config like:

```yaml
tmux:
  session_name: fleet
  config_file: ~/.config/fleet/tmux.conf   # optional

repos:
  - name: backend
    origin: git@github.com:org/backend.git
    base: ~/dev/.fleet-base/backend
    default_branch: main
    runtime: linux
    run:
      - name: api
        cmd: "npm run start:dev"

  - name: admin-ui
    origin: git@github.com:org/admin-ui.git
    base: ~/dev/.fleet-base/admin-ui
    default_branch: main
    runtime: windows
    run:
      - name: dev
        cmd: "npm run dev"
```

at `~/.config/fleet/repos.yaml`, then the usual flow is:

```sh
fleet-task new              # PROJ-1234, pick repos + patches -> worktrees ready
fleet-run start --ticket PROJ-1234   # pick which apps to bring up

# ...later, switch to a different ticket's apps without stopping PROJ-1234's worktrees:
fleet-run switch --to PROJ-5678

# jump a shell into any task's worktree (needs `fj`, below)
fj

fleet-task rm PROJ-1234     # done: remove worktrees + state
```

Put all three binaries on `PATH` (`fleet-cache` in particular must be reachable, since
`fleet-task new` shells out to it). Source the shell helper for jumping between worktrees, since a
subprocess can't `cd` its parent shell:

```sh
source /path/to/fleet-task/contrib/fleet.sh
```

## Development

Each CLI is an independent Go module:

```sh
cd fleet-task && go build ./... && go test ./...
cd fleet-run   && go build ./... && go test ./...
cd fleet-cache && go build ./... && go test ./...
```

Shell glue tests (requires [bats-core](https://github.com/bats-core/bats-core)):

```sh
cd fleet-task/contrib && bats fleet.bats fzf_integration.bats
```
