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
| Hooks dir           | `<config dir>/hooks/{pre-create,post-create,pre-remove,post-remove,pre-run,post-run}/*` | -         |
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
  branch_template: "eng/{ticket}"               # used when a repo omits `branch_template`; default "{ticket}"
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
    branch_template: "{ticket}-{description}"     # explicit `branch_template` overrides defaults.branch_template
    runtime: windows
    windows_base: ~/dev/.fleet-base-windows/admin-ui-custom   # explicit windows_base overrides defaults.windows_base_root
    run:
      - name: dev
        cmd: "npm run dev"
```

Field notes:
- `defaults.default_branch` / `defaults.branch_template` / `defaults.base_root` /
  `defaults.windows_base_root`: applied to every repo that omits the corresponding field, via
  `applyDefaults()` in both `fleet-task/config.go` and `fleet-run/config.go` (called right after
  YAML unmarshal, before the config is used for anything). A repo's own
  `base`/`default_branch`/`branch_template`/`windows_base`, when set, always wins over the default.
  `windows_base_root` only ever fills in `windows_base` for `runtime: windows` repos — a
  windows-runtime repo never gets a `base` derived from `defaults.base_root`, since it doesn't use
  one. Because `applyDefaults()` runs at parse time, everything downstream (`cmd_new.go`,
  `cmd_rm.go`, etc.) keeps reading `repo.Base`/`repo.DefaultBranch`/`repo.BranchTemplate`/
  `repo.WindowsBase` as plain, already-resolved fields — no caller needs to know defaults exist.
  (`fleet-run` has no `branch_template` field at all and ignores it — branch naming is entirely a
  `fleet-task new`-time concern.)
- `branch_template`: the git branch name `fleet-task new` creates is computed by `branchName()`
  (`fleet-task/branch.go`) from this template, substituting `{ticket}` (verbatim) and
  `{description}` (lowercased, non-alphanumeric runs collapsed to `-`, capped at 40 chars). An
  empty/unset template (the default) is equivalent to `"{ticket}"` — today's plain-ticket branch
  name. Note: if `{description}` is used and a later `fleet-task new` run for the *same* ticket is
  given a different description, the computed branch name changes too, so the "reattach to the
  existing branch on rerun" fallback (below) won't find a match and a new branch gets created
  instead — a known, accepted tradeoff of allowing description-based names.
- `origin` + `base`: on first use `fleet-task` runs `git clone --bare <origin> <base>` (a *bare*
  clone has a git history but no checked-out files, and is shared by every ticket's worktrees). On
  every subsequent `fleet-task new` it runs `git -C <base> fetch origin`, then
  `git -C <base> worktree add <worktree_path> -b <branch> origin/<default_branch>`, where `<branch>`
  is the `branch_template`-derived name above (not necessarily the raw ticket id).
- `runtime: windows` means: run commands for this repo are executed via
  `powershell.exe -NoExit -Command "..."` from WSL2, and its node_modules cache lives under a
  separate Windows-side cache path (hardlinks can't cross the WSL 9P boundary, the file-sharing
  protocol WSL2 uses to reach Windows drives). `fleet-cache`, `fleet-run` and `fleet-task` all use
  this field: in `fleet-task`, the bare clone and git worktree for a `runtime: windows` repo
  are created entirely by **Windows-native `git.exe`**, invoked from WSL via `powershell.exe -NoProfile -Command "<script>"` (the script
  `cd`s into the Windows directory, runs `git`, and propagates its exit code; see
  `fleet-task/winpath.go`'s `windowsCommandScript`), against paths under
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
    ensure-windows`'s hardlink step fails. Each tool checks what it can see, up front
    (`checkSameDrive` in each module's `winpath.go`), and errors clearly rather than letting it fail
    per-file inside a PowerShell script: `fleet-task` (`windowsGit.Preflight`) checks
    `windows_base` against the real worktree location under `windows_worktree_root` (it never
    looks at the cache root); `fleet-cache ensure-windows` checks the resolved cache root against
    the target worktree. On Linux/WSL, `fleet-cache ensure` likewise compares the filesystem
    device of the cache root and the target directory before running `npm ci`.
- SSH remotes with passphrase-protected keys will hang in headless/non-interactive invocations.
  `fleet-task` should precheck `ssh-add -l` when `origin` looks like an SSH URL (`git@` or
  `ssh://`) and print a warning (not silently hang) if no identity is loaded. This check is WSL-side
  only and is skipped for `runtime: windows` repos, since Windows git.exe manages its own SSH
  credentials independently (Git for Windows / Windows OpenSSH), not WSL's `ssh-add`.

## Patches

Directory listing only, no index file:
`<config dir>/patches/<repo>/*.patch` — plain files produced by `git diff` / `git format-patch`.
`fleet-task new` lets the user multiselect (in-process bubbletea picker, by filename) which ones
to apply per repo, applying each with `git apply` in the new worktree (never `git am`; authorship/
commit metadata in a patch is not preserved).

## Hooks

`<config dir>/hooks/{pre-create,post-create,pre-remove,post-remove,pre-run,post-run}/*` — any
executable file found is run in filename-sorted order. Environment passed to each hook:

```
FLEET_TICKET=<ticket id>
FLEET_REPO=<repo name>
FLEET_WORKTREE_DIR=<absolute path>
```

`pre-create`/`post-create` run around `fleet-task new`'s per-repo worktree setup, and around
`fleet-task edit <ticket>`'s per-repo setup for any repos it adds (same underlying per-repo
function, `createRepoWorktree`). The per-repo order is: `pre-create` hooks → ensure the base bare
clone exists (clone or fetch) → `git worktree add` → apply patches → `post-create` hooks → (if the
worktree has a `package-lock.json`) `fleet-cache ensure`. Because `pre-create` runs first, those
hooks must not assume the base clone exists yet.

`pre-remove`/`post-remove` run around `fleet-task rm`'s per-repo teardown, and around `fleet-task
edit <ticket>`'s per-repo teardown for any repos it removes (same underlying per-repo function,
`removeRepoWorktree`). `FLEET_WORKTREE_DIR` for these is the worktree path that is about to be (or
was) removed; by the time `post-remove` runs, that path is generally gone from disk. Like
pre-create/post-create, a failing hook only warns to stderr and never blocks the removal.

`pre-run`/`post-run` run around `fleet-run start`'s per-window launch (reserved for fleet-run;
not required for v1 but the directories/contract should exist).

## Per-task state file: `tasks/<ticket>.json`

One file per ticket — never a single shared file — so two `fleet-task`/`fleet-run` invocations for
*different* tickets never contend on the same file. `fleet-task` writes a given ticket's own file
under an advisory lock as a safety belt: a `<file>.lock` sibling created with `O_EXCL` (not
`flock(2)`) that contains the owner's pid. A lock whose pid is no longer alive, or that is older
than 5 minutes, is treated as abandoned and taken over; otherwise writers wait up to 5 seconds and
then fail with an error naming the lock file. Writes themselves go via a temp file + rename.
Everything else (`fleet-run`) only reads these files.

```json
{
  "ticket": "PROJ-1234",
  "description": "Add retry logic to payment webhook",
  "created_at": "2026-08-02T22:58:00Z",
  "repos": [
    { "repo": "backend", "branch": "eng/PROJ-1234", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/backend" },
    { "repo": "admin-ui", "branch": "PROJ-1234-add-retry-logic-to-payment-webhook", "worktree_path": "/home/jon/.local/state/fleet/worktrees/PROJ-1234/admin-ui" }
  ]
}
```

`branch` reflects each repo's resolved `branch_template` (see `repos.yaml` field notes above) — it's
not necessarily identical to `ticket`, and can differ per repo. `worktree_path` is always
`<worktree_root>/<ticket>/<repo>` regardless of the branch name.

`repos` isn't fixed at `fleet-task new` time — `fleet-task edit <ticket>` can add or remove entries
from it later, reusing `new`'s per-repo setup and `rm`'s per-repo teardown respectively. Repos added
via `edit` reuse the task's existing top-level `description` for `branch_template` resolution rather
than re-prompting, so branch names stay consistent with repos added at `new` time. If `edit` removes
every repo and adds none, the state file is deleted entirely rather than left with an empty `repos`
array.

`fleet-task list`/`fleet-task jump` and `fleet-run` all build their view of "what tickets/worktrees
currently exist" by globbing `tasks/*.json` — never by reading a single aggregate file.

A ticket id is not permanently fixed once created: `fleet-task mv <old> <new>` renames it in
place — moving `tasks/<old>.json` to `tasks/<new>.json` (updating `ticket` to match), and every
repo's worktree from `<worktree_root>/<old>/<repo>` to `<worktree_root>/<new>/<repo>` via
`git worktree move` (so git's own worktree registration stays consistent, unlike a plain filesystem
rename). It refuses to touch a ticket that's currently active under `fleet-run` (same guard `rm`
uses). It does **not** rename `branch` — branch names come from `branch_template` and aren't
necessarily derived from the ticket at all, so they're left as-is even after a rename.

## tmux session & window naming

- Session name is the single fixed value from `repos.yaml` `tmux.session_name` (e.g. `fleet`).
  `fleet-run` creates it, if it doesn't already exist, the first time it creates a window in it —
  folding session creation into that first `new-session -d -s <name> -f <config_file> -n <name> ...`
  call rather than a separate content-less `new-session` — and reuses it for every ticket
  thereafter. This is deliberate: a bare `new-session` with no window/command gives tmux the
  chance to create its own implicit default window (which it names `bash`), which would show up
  in `fleet-run stop`'s picker and never get cleaned up, breaking both that picker and the
  "nothing left running" detection below.
- **Only one ticket's windows are ever live in the session at a time** — the point of `fleet-run`
  is switching the whole runtime environment to a different work context, not running several
  tickets' app sets side by side. `fleet-run start --ticket X` for any ticket other than whatever's
  currently active stops every window in the session first, then starts `X`'s selection; starting
  the *same* ticket that's already active is additive (only missing windows get created), same as
  reruns always worked. There is no separate `switch` command — this is `start`'s only behavior now.
- Because only one ticket is ever running, each window is named just `<repo>-<run-name>`, e.g.
  `backend-api` — no ticket prefix. This is how `fleet-run stop` finds and kills the right windows —
  never by session name, since the session itself never changes. On the command line and in
  pickers the same target is written `repo:run-name`. To keep both forms unambiguous,
  `fleet-run` validates `repos.yaml` on load: it rejects `:` in any repo or run name, and rejects
  any two repo/run pairs whose `<repo>-<run-name>` window names collide (repo `admin` + run
  `ui-dev` vs repo `admin-ui` + run `dev`, or duplicate run names in one repo). `fleet-task` does
  **not** enforce this (it never builds window names), so a config `fleet-task` accepts can still
  be rejected by `fleet-run`.
- `tmux.config_file` is passed as `tmux -f <file>`, a global flag that tmux only honours when it
  starts the server; if a server is already running the file is not re-read.
- **Which ticket is active is tracked via two global (`set-option -g`) tmux user options** — not
  per-session ones; fleet only ever has the one session, and `-g` avoids tmux target resolution
  needing an attached client. `start` sets them right after the session's first window exists
  (tmux needs a running server first), and `stop` clears them once nothing is left running: `@fleet_task_ticket` (the ticket id) and
  `@fleet_task_description` (its description). Nothing else derives "what's currently active" from
  window names anymore — `fleet-run stop --ticket X` uses these options purely as a safety
  assertion (errors out, stopping nothing, if `X` isn't the active ticket) rather than as a filter,
  since there's nothing else running to filter out. These options are also there for your own tmux
  config to read directly in a status-line format string, e.g.
  `#{@fleet_task_ticket}: #{@fleet_task_description}`. `fleet-task` reads `@fleet_task_ticket` too
  (`tmux_check.go`, a plain `tmux show-options -g -v` shell-out, not shared code): `rm` and `edit`'s
  repo-removal path refuse to tear down a worktree for the ticket fleet-run currently has live,
  since a running dev server can race the delete and write into its own untracked cache dir (e.g.
  Vite's `.vite/deps_temp_*`) at the exact moment git tries to remove it, leaving `git worktree
  remove --force` failing with "Directory not empty" — the fix is `fleet-run stop` first, not a
  smarter retry.

## CLI contracts between tools (subprocess boundary, not shared code)

- `fleet-task new` shells out to `fleet-cache ensure <worktree_dir>` after creating each repo's
  worktree, if that repo's dir has a `package-lock.json` — or, for `runtime: windows` repos,
  `fleet-cache ensure-windows [--cache-root <windows_cache_root>] <worktree_dir>` instead (flags
  may appear before or after `<dir>`; `fleet-task` happens to put them first). Both
  exit 0 on success, non-zero with a message on stderr on failure; `fleet-task` should not abort
  the whole `new` on a cache failure for one repo — warn and continue (node_modules install can be
  retried manually). `fleet-cache`'s progress messages and npm's output go to **stderr**, so
  stdout stays clean for results. `ensure-windows` runs `npm ci`, the pre-install `node_modules` removal, and the
  hardlink population all as native Windows processes (PowerShell/npm) — only single-file
  operations (hashing the lockfile, copying `package.json`/`package-lock.json` into the cache
  entry) stay as plain Go file I/O against the `/mnt/...` path, since those are negligible one-off
  costs rather than the many-small-file operations that are slow across the WSL 9P boundary.
  `gc-windows` mirrors `gc`'s `--roots`/`--force`/`--json` semantics against the Windows-native
  cache root instead. For both `gc` commands, every `--roots` directory must exist (a typo is an
  error, not an empty root), and without both `--roots` and `--force` they only report.
- Cache entries are built in a staging directory `<cache root>/node-cache/<hash>.tmp-<pid>` and
  renamed to `<hash>` only once complete, so a `<hash>` directory is always a finished entry and
  concurrent `ensure`s of one hash can't corrupt each other. Staging dirs left behind by a killed
  run are reported by `gc` (and removed with `--force --roots`) once they're older than a day.
- `fleet-cache ensure` (Linux/WSL2 path only) is safe to call repeatedly against the same
  `<worktree_dir>`: it's a no-op if `<worktree_dir>/node_modules` is already linked from the current
  `package-lock.json` hash (tracked via a `.fleet-cache-hash` marker inside `node_modules`), so
  anything added to `node_modules` by hand since the last `ensure` — most notably an `npm link`
  package symlink — survives untouched. It only rebuilds `node_modules` (destroying manual changes,
  same as before) when the lockfile hash actually changes, or when called with `--force`. Regular
  files in a cache entry are also chmod'd read-only (`0444`) once, at `npm ci` time — since
  permission bits live on the inode shared by every worktree's hardlink, this doesn't block
  `ensure`'s own rebuild or `npm link`'s symlink-swap (both only need write access to the containing
  directory), but does turn any future in-place edit to a hardlinked file into a loud permission
  error instead of silently corrupting every other worktree sharing that lockfile hash. This does
  not apply to `ensure-windows`: its hardlinks are made by PowerShell (`New-Item -ItemType
  HardLink`, via the embedded `hardlink_windows.ps1`) and the entry's file permissions are not
  locked down. `ensure-windows` has the same skip-marker and `--force` semantics as `ensure`.
- `fleet-task jump` prints the chosen worktree path (and only that, no other output) on stdout so
  it composes with a shell function:
  ```bash
  fj() { local d; d=$(fleet-task jump) && cd "$d"; }
  ```
- Machine-readable output: `--json` is supported by `fleet-task list`, `fleet-run status`, and
  `fleet-cache gc`/`gc-windows` (dry-run and `--force` alike). Nothing else has it: `fleet-task
  jump` prints a single bare path on stdout for `cd`, and the mutating commands (`new`, `edit`,
  `mv`, `rm`, `start`, `stop`, `ensure*`) are not meant to be parsed. Exit status is 0 on success and
  1 on any failure, including usage errors and *partial* failure (every repo/window is attempted
  before exiting 1) — in all three tools.
