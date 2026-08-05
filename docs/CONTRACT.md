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
| Windows base clone  | `repos.yaml` `windows_base` (or `defaults.windows_base_root`/<repo>) | -      |
| Windows worktree root | `repos.yaml` top-level `windows_worktree_root` (required for `runtime: windows`) | - |
| Windows cache root  | `repos.yaml` top-level `windows_cache_root`, else `%LOCALAPPDATA%\fleet` | -   |
| Windows node cache entries | `<windows cache root>\node-cache\<sha256 of package-lock.json>` | -    |

All four Windows-side paths above are still expressed in **WSL-visible form** (e.g.
`/mnt/c/fleet/base`), the same convention as every other path in `repos.yaml` — they're only
converted to win32 (`C:\...`) form via `wslpath -w` at the point of invoking a Windows-native
command. `windows_cache_root` is the one exception that can *originate* on the Windows side (the
`%LOCALAPPDATA%` auto-default), in which case it's converted back to WSL form via `wslpath -u`
before use.

## `repos.yaml`

```yaml
tmux:
  session_name: fleet             # fixed session name shared by ALL tickets; never renamed per-ticket
  config_file: ~/.config/fleet/tmux.conf   # optional; passed to `tmux -f` on session create

worktree_root: ~/.local/state/fleet/worktrees   # optional override of the default state-dir location
windows_worktree_root: ~/dev/.fleet-worktrees-windows   # required if any repo is runtime: windows
windows_cache_root: ~/dev/.fleet-cache-windows          # optional; else auto-detected via %LOCALAPPDATA%

defaults:                                       # optional; fills in blank per-repo fields below
  default_branch: main                          # used when a repo omits `default_branch`
  base_root: ~/dev/.fleet-base                  # used when a repo omits `base`: base = <base_root>/<repo-name>
  windows_base_root: ~/dev/.fleet-base-windows  # used when a runtime: windows repo omits `windows_base`

repos:
  - name: backend
    origin: git@github.com:org/backend.git      # git remote URL fleet-task clones from on first use
    runtime: linux                                # linux | windows
    run:
      - name: api
        cmd: "npm run start:dev"
      - name: worker
        cmd: "npm run start:worker"

  - name: admin-ui
    origin: git@github.com:org/admin-ui.git
    base: ~/dev/.fleet-base/admin-ui-custom       # explicit `base` overrides defaults.base_root
    default_branch: develop                       # explicit `default_branch` overrides defaults.default_branch
    runtime: windows
    windows_base: ~/dev/.fleet-base-windows/admin-ui-custom   # explicit windows_base overrides defaults.windows_base_root
    run:
      - name: dev
        cmd: "npm run dev"
```

Field notes:
- `defaults.default_branch` / `defaults.base_root` / `defaults.windows_base_root`: applied to
  every repo that omits the corresponding field, via `applyDefaults()` in both
  `fleet-task/config.go` and `fleet-run/config.go` (called right after YAML unmarshal, before the
  config is used for anything). A repo's own `base`/`default_branch`/`windows_base`, when set,
  always wins over the default. `windows_base_root` only ever fills in `windows_base` for
  `runtime: windows` repos — a windows-runtime repo never gets a `base` derived from
  `defaults.base_root`, since it doesn't use one. Because `applyDefaults()` runs at parse time,
  everything downstream (`cmd_new.go`, `cmd_rm.go`, etc.) keeps reading
  `repo.Base`/`repo.DefaultBranch`/`repo.WindowsBase` as plain, already-resolved fields — no caller
  needs to know defaults exist.
- `origin` + `base`: on first use `fleet-task` runs `git clone --bare <origin> <base>`. On every
  subsequent `fleet-task new` it runs `git -C <base> fetch origin`, then
  `git -C <base> worktree add <worktree_path> -b <ticket> origin/<default_branch>`.
- `runtime: windows` means: run commands for this repo are executed via
  `powershell.exe -NoExit -Command "..."` from WSL2, and its node_modules cache lives under a
  separate Windows-side cache path (hardlinks can't cross the WSL9P boundary). `fleet-cache` and
  `fleet-run` both need this field. As of this contract revision, `fleet-task` also needs it: the
  bare clone and git worktree for a `runtime: windows` repo are created entirely by **Windows-native
  `git.exe`**, invoked from WSL via `powershell.exe -NoProfile -Command "cd '<winDir>'; & git ...;
  exit $LASTEXITCODE"` (see `fleet-task/winpath.go`'s `runWindowsCommand`), against paths under
  `windows_base`/`windows_worktree_root` rather than `base`/`worktree_root`. This keeps the actual
  checkout on an NTFS volume instead of WSL git writing through the 9P bridge into what's nominally
  a Windows-mounted path — the original bug this whole mechanism exists to fix.
  - **The WSL-side worktree location becomes a symlink.** `fleet-task new` still creates
    `<worktree_root>/<ticket>/<repo>` for a `runtime: windows` repo — every existing consumer
    (`fleet-run`, hooks, `fleet-cache`) still expects to find the worktree there — but it's a
    **symlink**, not a real directory, pointing at the WSL-visible view of the real Windows-native
    worktree under `windows_worktree_root`. `TaskRepo.WorktreePath` in `tasks/<ticket>.json` is
    unchanged in shape (still a plain path string); it's just a symlink now for these repos.
    `fleet-task rm` tears this down symmetrically: it resolves the symlink, deregisters the real
    Windows-native worktree via `git.exe`, then removes the symlink itself — removing only the
    symlink would leak the real tree and its worktree registration.
  - **`fleet-run`'s `newWindow` (tmux_exec.go) must resolve that symlink before calling
    `wslpath -w`.** `wslpath` does pure syntactic path translation and does not follow symlinks; on
    a symlinked worktree it would otherwise map the symlink's own WSL-ext4-side path to a
    `\\wsl.localhost\...` UNC path instead of the correct `C:\...` location. This is implemented via
    `filepath.EvalSymlinks` before `wslToWindowsPath` (a no-op for any windows-runtime worktree that
    isn't a symlink) — do not remove this thinking it's redundant.
  - **NTFS hardlinks require the same drive.** `windows_base`, `windows_worktree_root`, and the
    resolved windows cache root must all resolve to the same drive letter, or `fleet-cache
    ensure-windows`'s hardlink step fails. Both `fleet-task` and `fleet-cache` check this up front
    (`checkSameDrive` in each module's `winpath.go`) and error clearly rather than let it fail
    per-file inside a PowerShell script.
- SSH remotes with passphrase-protected keys will hang in headless/non-interactive invocations.
  `fleet-task` should precheck `ssh-add -l` when `origin` looks like an SSH URL (`git@` or
  `ssh://`) and print a warning (not silently hang) if no identity is loaded. This check is WSL-side
  only and is skipped for `runtime: windows` repos, since Windows git.exe manages its own SSH
  credentials independently (Git for Windows / Windows OpenSSH), not WSL's `ssh-add`.

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
  worktree, if that repo's dir has a `package-lock.json` — or, for `runtime: windows` repos,
  `fleet-cache ensure-windows <worktree_dir> [--cache-root <windows_cache_root>]` instead. Both
  exit 0 on success, non-zero with a message on stderr on failure; `fleet-task` should not abort
  the whole `new` on a cache failure for one repo — warn and continue (node_modules install can be
  retried manually). `ensure-windows` runs `npm ci`, the pre-install `node_modules` removal, and the
  hardlink population all as native Windows processes (PowerShell/npm) — only single-file
  operations (hashing the lockfile, copying `package.json`/`package-lock.json` into the cache
  entry) stay as plain Go file I/O against the `/mnt/...` path, since those are negligible one-off
  costs rather than the many-small-file operations that are slow across the WSL9P boundary.
  `gc-windows` mirrors `gc`'s `--roots`/`--force` safety semantics against the Windows-native cache
  root instead.
- `fleet-task jump` prints the chosen worktree path (and only that, no other output) on stdout so
  it composes with a shell function:
  ```bash
  fj() { local d; d=$(fleet-task jump) && cd "$d"; }
  ```
- All three CLIs support `--json` on read commands (`list`, `jump` uses plain stdout since it's a
  single path meant for `cd`) so they can be piped into `jq`/`fzf` by users directly, in keeping
  with composing these tools as plain Unix pieces rather than a monolith.
