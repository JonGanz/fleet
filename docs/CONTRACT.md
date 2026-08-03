# Fleet shared contract

Three independent Go CLIs — `fleet-task`, `fleet-run`, `fleet-cache` — coordinate purely
through files on disk (config the user authors, state the tools own) and by shelling out
to each other as subprocesses. No shared Go code/module between them.

## Paths (XDG-compliant, all overridable via `FLEET_*` env vars for testing)

| Purpose            | Path                                                    | Env override      |
|---------------------|---------------------------------------------------------|--------------------|
| Config dir          | `$XDG_CONFIG_HOME/fleet` (default `~/.config/fleet`)     | `FLEET_CONFIG_DIR` |
| Repos config file   | `<config dir>/repos.yaml`                                | `FLEET_REPOS_FILE` |
| Patches dir         | `<config dir>/patches/<repo>/<patch-name>.patch`         | -                  |
| Hooks dir           | `<config dir>/hooks/{pre-create,post-create,pre-run,post-run}/*` | -         |
| State dir           | `$XDG_STATE_HOME/fleet` (default `~/.local/state/fleet`) | `FLEET_STATE_DIR`  |
| Per-task state file | `<state dir>/tasks/<ticket>.json`                        | -                  |
| Worktree root       | `<state dir>/worktrees/<ticket>/<repo>` (or `repos.yaml` `worktree_root`) | -   |
| Cache dir           | `$XDG_CACHE_HOME/fleet` (default `~/.cache/fleet`)        | `FLEET_CACHE_DIR`  |
| Node cache entries  | `<cache dir>/node-cache/<sha256 of package-lock.json>`   | -                  |

## `repos.yaml`

```yaml
tmux:
  session_name: fleet             # fixed session name shared by ALL tickets; never renamed per-ticket
  config_file: ~/.config/fleet/tmux.conf   # optional; passed to `tmux -f` on session create

worktree_root: ~/.local/state/fleet/worktrees   # optional override of the default state-dir location

repos:
  - name: backend
    origin: git@github.com:org/backend.git      # git remote URL fleet-task clones from on first use
    base: ~/dev/.fleet-base/backend              # shared bare clone; every worktree branches off this
    default_branch: main
    runtime: linux                                # linux | windows
    run:
      - name: api
        cmd: "npm run start:dev"
      - name: worker
        cmd: "npm run start:worker"

  - name: admin-ui
    origin: git@github.com:org/admin-ui.git
    base: ~/dev/.fleet-base/admin-ui
    default_branch: main
    runtime: windows
    run:
      - name: dev
        cmd: "npm run dev"
```

Field notes:
- `origin` + `base`: on first use `fleet-task` runs `git clone --bare <origin> <base>`. On every
  subsequent `fleet-task new` it runs `git -C <base> fetch origin`, then
  `git -C <base> worktree add <worktree_path> -b <ticket> origin/<default_branch>`.
- `runtime: windows` means: run commands for this repo are executed via
  `powershell.exe -NoExit -Command "..."` from WSL2, and its node_modules cache lives under a
  separate Windows-side cache path (hardlinks can't cross the WSL9P boundary). `fleet-cache` and
  `fleet-run` both need this field.
- SSH remotes with passphrase-protected keys will hang in headless/non-interactive invocations.
  `fleet-task` should precheck `ssh-add -l` when `origin` looks like an SSH URL (`git@` or
  `ssh://`) and print a warning (not silently hang) if no identity is loaded.

## Patches

Directory listing only, no index file:
`<config dir>/patches/<repo>/*.patch` — plain files produced by `git diff` / `git format-patch`.
`fleet-task new` lets the user multiselect (fzf) which ones to apply per repo, applying each with
`git apply` (or `git am` if the file has commit-message headers) in the new worktree.

## Hooks

`<config dir>/hooks/{pre-create,post-create,pre-run,post-run}/*` — any executable file found is
run in filename-sorted order. Environment passed to each hook:

```
FLEET_TICKET=<ticket id>
FLEET_REPO=<repo name>
FLEET_WORKTREE_DIR=<absolute path>
```

`pre-create`/`post-create` run around `fleet-task new`'s per-repo worktree setup.
`pre-run`/`post-run` run around `fleet-run start`'s per-window launch (reserved for fleet-run;
not required for v1 but the directories/contract should exist).

## Per-task state file: `tasks/<ticket>.json`

One file per ticket — never a single shared file — so two `fleet-task`/`fleet-run` invocations for
*different* tickets never contend on the same file. Writes to a given ticket's own file should
still take a `flock` on that file as a safety belt.

```json
{
  "ticket": "PROJ-1234",
  "description": "Add retry logic to payment webhook",
  "created_at": "2026-08-02T22:58:00Z",
  "repos": [
    { "repo": "backend", "branch": "PROJ-1234", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/backend" },
    { "repo": "admin-ui", "branch": "PROJ-1234", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/admin-ui" }
  ]
}
```

`fleet-task list`/`fleet-task jump` and `fleet-run` all build their view of "what tickets/worktrees
currently exist" by globbing `tasks/*.json` — never by reading a single aggregate file.

## tmux session & window naming

- Session name is the single fixed value from `repos.yaml` `tmux.session_name` (e.g. `fleet`).
  `fleet-run` creates it once (`tmux new-session -d -s <name> -f <config_file>` if `config_file`
  is set) if it doesn't already exist, and reuses it for every ticket thereafter.
- Each running app is its own window named `<ticket>-<repo>-<run-name>`, e.g.
  `PROJ-1234-backend-api`. This is how `fleet-run stop`/`switch` find and kill the right windows —
  never by session name, since the session itself never changes.

## CLI contracts between tools (subprocess boundary, not shared code)

- `fleet-task new` shells out to `fleet-cache ensure <worktree_dir>` after creating each repo's
  worktree, if that repo's dir has a `package-lock.json`. `fleet-cache ensure` exits 0 on success,
  non-zero with a message on stderr on failure; `fleet-task` should not abort the whole `new` on a
  cache failure for one repo — warn and continue (node_modules install can be retried manually).
- `fleet-task jump` prints the chosen worktree path (and only that, no other output) on stdout so
  it composes with a shell function:
  ```bash
  fj() { local d; d=$(fleet-task jump) && cd "$d"; }
  ```
- All three CLIs support `--json` on read commands (`list`, `jump` uses plain stdout since it's a
  single path meant for `cd`) so they can be piped into `jq`/`fzf` by users directly, in keeping
  with composing these tools as plain Unix pieces rather than a monolith.
