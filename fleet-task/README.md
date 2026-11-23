# fleet-task

`fleet-task` creates and manages per-ticket sets of git worktrees (extra
working directories sharing one git history), one
per repo the ticket touches, so several tickets (and their AI-agent
pipelines) can be worked in parallel without their working trees
conflicting. It's one of three independent CLIs (`fleet-task`, `fleet-run`,
`fleet-cache`) in the fleet suite; see `../docs/CONTRACT.md` for the full
shared contract (paths, `repos.yaml` schema, state file format, hook
conventions).

## Commands

### `fleet-task new [ticket] [-d <description>] [--repo <name>]... [--skip-patch <repo>:<name>]... [--no-patches] [--json]`

**Flag mode** (for scripts and agents): with a ticket and at least one
`--repo` nothing is prompted. `--repo` is repeatable or comma-separated;
`-d`/`--description` defaults to empty. Every available patch is applied to
each repo — the same default as the picker's all-checked start — minus any
`--skip-patch <repo>:<name>`, or none with `--no-patches`. Unknown repos or
patch names are usage errors (exit 2) raised before anything is created.
Each repo's branch name (from `branch_template`) is validated up front too: an
empty name or one `git check-ref-format --branch` rejects is a usage error
naming the repo and template. Applied patch names are recorded per repo in the state file. A patch that
fails to apply is a warning, not a failure: it is listed in the `--json`
result's `patch_errors` and left out of the recorded `patches`.

**Interactively** (no `--repo`):

1. Prompts for a ticket id and description. The ticket id is validated (see
   "Flags, help, and exit codes"), and `new` **refuses a ticket that already
   has a state file**: rerunning it would overwrite the state with only this
   run's repos and orphan the earlier worktrees. Use `fleet-task edit
   <ticket>` to add repos to an existing task, or `rm` it first.
2. Loads `repos.yaml` and lets you multiselect which repos this task
   touches, via an in-process picker (vim keys: `j`/`k` move, `gg`/`G` top/
   bottom, `space` toggle and advance to the next row, `a` select all, `A`
   clear, `/` filter, `enter` confirm, `esc`/`q` cancel).
3. For each selected repo with patch files under
   `<config dir>/patches/<repo>/*.patch`, lets you multiselect which to
   apply — every patch starts checked, so you uncheck the ones you don't
   want rather than checking the ones you do (skipped entirely if the
   repo has no patches). All repos' patch prompts happen up front, before
   any directory/worktree work starts for any of them, so the whole
   interactive part of `new` front-loads instead of interleaving prompts
   with git/hook/cache work.
4. For each selected repo, in order:
   - Runs `pre-create` hooks. These run *first*, before the repo's base
     clone is ensured, so they must not assume the base exists yet.
   - Ensures the repo's shared bare clone (`base` in `repos.yaml`; a clone with
     history but no checked-out files) exists and is current: created on first
     use (`git init --bare` + `remote add origin` + `fetch`, so no local
     branches are copied from the remote), then `git fetch --prune origin`
     every time. Setup is idempotent: a base left half-made by an interrupted
     first run (an empty directory, or a repo with no `origin` remote yet) is
     completed; a non-empty directory that isn't a git repo is refused, never
     initialised over. If the base's `origin` URL differs from `repos.yaml`'s,
     `new` warns and leaves it alone (it prints the `remote set-url` command).
     If `origin` looks like an SSH URL, runs `ssh-add -l` before the first
     clone/fetch (once per run) and warns (but doesn't block) if no identity
     looks loaded. Without a terminal, WSL-side git runs with
     `GIT_TERMINAL_PROMPT=0`, so an HTTPS credential prompt fails instead of
     hanging.
   - Creates a worktree at `<worktree_root>/<ticket>/<repo>` on a branch
     computed from `branch_template` (repos.yaml, per-repo or fleet-wide
     default — see "Design notes" below; defaults to the plain ticket id if
     unset). In order: if a local branch of that name exists (a rerun, or a
     repo re-added by `edit`) it is checked out; else if `origin/<branch>`
     exists (someone already pushed it) a local branch tracking it is
     created; else a new branch is created from `origin/<default_branch>`
     with **no upstream** (so a plain `git push` can't target the default
     branch). A missing `origin/<default_branch>` (e.g. a typo'd
     `default_branch`) is reported by name.
   - Applies the selected patches with `git apply` (see "Design notes"
     below for why `apply` rather than `am`).
   - Runs `post-create` hooks.
   - If the new worktree has a `package-lock.json`, shells out to
     `fleet-cache ensure <worktree_path>` (must be on `PATH`); a cache
     failure warns but doesn't abort the rest of `new`.
   - Writes `<state dir>/tasks/<ticket>.json` right after each repo succeeds,
     so an interrupted `new` (Ctrl-C during a long `npm ci`) still leaves a
     task listing every worktree created so far, which `edit --add` / `rm`
     can then deal with.
5. If some repos failed, every repo is still attempted, the successful ones
   are recorded, and `new` exits 1 at the end; add the failed ones later
   with `fleet-task edit`. If none succeeded, no state file is written.

### `fleet-task list [--status] [--json]`

Globs `<state dir>/tasks/*.json` and prints a table of
ticket/description/repo-count/created-at, or the raw JSON array with
`--json`. A file whose `ticket` field disagrees with its filename is listed
under the filename (with a warning): the filename is what every command
looks up.

`--status` adds a CHANGES column (`clean`, `2 dirty`, `1 dirty, 1 unknown`)
and, in JSON, `dirty_repos`/`unknown_repos` per task, using the same check
as `show`. It is opt-in because it runs several git commands in every
worktree (PowerShell round trips for `runtime: windows` repos).

### `fleet-task show <ticket> [--json]`

One task: description, created-at, whether `fleet-run` currently has it
active, and per repo its branch, worktree path, applied patches,
uncommitted changes and unpushed commits. In the JSON (`{ticket,
description, created_at, repos[{repo, branch, worktree_path, patches,
changes, unpushed_commits}], active}`), `patches` is `null` for tasks
created before patches were recorded ("unknown") and `[]` when none were
applied; the text table shows `?` and `-` respectively.

**Uncommitted changes** exclude the patches fleet-task applied itself
(`git apply` leaves them uncommitted by design, so a plain `git status` would
flag nearly every worktree). `changes` is `{state, files[{status, path}],
patches_excluded, approximate, reason?}`, `state` being `clean`, `dirty` or
`unknown`; the table shows `clean`, `3 changed` (`~3 changed` if
approximate) or `unknown`.

- `runtime: linux` (exact): two trees are built with throwaway index files —
  never touching the worktree or its real index — *expected* = `HEAD` plus
  each recorded patch (`read-tree HEAD`, `apply --cached`, `write-tree`) and
  *actual* = the worktree (`add -A`, honouring `.gitignore`, `write-tree`);
  `diff-tree --name-status` between them lists the changes.
- `runtime: windows` (approximate): `git status --porcelain` via `git.exe`,
  minus every file a recorded patch touches (so an edit inside a patched file
  is not seen).
- If the recorded patch list is `null`, a patch file no longer exists under
  `<config dir>/patches/<repo>/`, or a patch no longer applies to `HEAD`
  (e.g. it was committed), changes are computed against plain `HEAD`,
  `patches_excluded` is `false` and `reason` says why. A missing worktree or
  a git failure gives `unknown` with a `reason` — never `clean`.

`unpushed_commits` (`git rev-list --count <branch> --not --remotes`; `null`
if it can't be determined) is informational only: the branch survives `rm`.

### `fleet-task repos [--json]`

Repos configured in `repos.yaml`: name, runtime, default branch, branch
template, `fleet-run` targets (`run[{name, target: "<repo>:<name>", cmd}]`)
and available patches (same objects as `fleet-task patches`).

### `fleet-task patches [repo] [--json]`

Patches under `<config dir>/patches/<repo>/*.patch` (every repo if none is
given), each as `{repo, name, path, summary, files}`:

- `summary` is the `Subject:` line of a `git format-patch` file (minus any
  `[PATCH n/m]` tag), else the first non-blank line of free text before the
  diff — `git apply` ignores that text, so a one-line description can be put
  at the top of a plain `git diff` patch — else `""`.
- `files` are the paths the patch touches, from its `diff --git` headers (or
  `+++` lines for a plain unified diff).

### `fleet-task jump [ticket] [--repo <name>]`

With `--repo` (which requires a ticket), or whenever only one worktree
matches, the path is printed without a picker. An unknown ticket, or a
`--repo` the ticket doesn't have, is a usage error (exit 2).

Globs `<state dir>/tasks/*.json`, flattens all (ticket, repo, worktree
path) rows across every task, and lets you pick one via the same
in-process picker as `new` (single-select: no checkboxes, `enter`
confirms the highlighted row). Prints *only* the chosen worktree path to
stdout (nothing else), so use it as `cd "$(fleet-task jump)"` (a
subprocess can't `cd` its parent shell). With a ticket argument, only that ticket's repos
are offered. If selection is cancelled (e.g. `Esc`/`q`),
exits non-zero with no stdout output.

### `fleet-task edit [ticket] [--add <repo>]... [--remove <repo>]... [--skip-patch <repo>:<name>]... [--no-patches] [--force] [--json]`

**Flag mode**: `--add`/`--remove` (repeatable or comma-separated) replace the
pickers. Added repos must be configured and not yet attached, removed repos
must be attached, and no repo may be in both — otherwise a usage error
(exit 2) before anything changes. Patches for added repos follow `new`'s
flag-mode rules.

Interactively, `edit` lets an already-created task's repo list grow or shrink, without tearing
down and recreating the whole task:

1. Loads the task's state file and `repos.yaml`, then shows two separate
   multiselect pickers (same in-process UI as `new`): one over repos in
   `repos.yaml` not yet part of the task ("select repos to add"), one over
   the task's currently-attached repos ("select repos to remove"). Either
   can be left empty — unlike `new`'s repo picker, confirming with nothing
   checked here means exactly that (no add/no remove), not "whatever repo
   the cursor happens to be on."
2. For repos being added, prompts for patch selection up front (same as
   `new`, and for the same reason — front-load every prompt before any
   git/hook work starts).
3. Removes the selected repos first, using the same per-repo teardown as
   `rm` (pre-remove/post-remove hooks and all — see below) and the same
   partial-failure handling: a repo that fails to remove stays in the
   task's state for the next `edit`/`rm` to retry, rather than silently
   vanishing from `fleet-task list`.
4. Adds the selected repos, using the same per-repo setup as `new` — the
   branch name is computed from the task's existing `description`, not
   re-prompted, so repos added later get names consistent with the ones
   added at `new` time.
5. Writes the updated state file. If every repo ends up removed and none
   were added, deletes the task's state file entirely instead of leaving an
   empty-repos file behind — matching `rm`'s end state.

Before removing anything, `edit` runs the same uncommitted-changes guard as
`rm` (see below) over the repos being removed; `--force` skips it.

`edit` refuses to *remove* repos while the ticket is the one currently active
in `fleet-run` (read from the `@fleet_task_ticket` tmux option), with a message
telling you to `fleet-run stop` first; adding repos is unaffected. If any
add or remove failed, `edit` still attempts everything and saves the state,
then exits 1. If nothing is selected it prints "no changes selected" on
stdout and exits 0.

### `fleet-task mv [old-ticket] [new-ticket] [--json]`

Renames a ticket in place, without tearing down and recreating its
worktrees:

1. If `old-ticket` is omitted, shows the same interactive ticket picker as
   `rm`. If `new-ticket` is omitted, prompts for it.
2. Refuses if `old-ticket` is currently active under `fleet-run` (same guard
   `rm`/`edit` use) — renaming a live ticket would desync `fleet-run`'s tmux
   state from the new filename.
3. Validates `new-ticket` (a valid ticket id, different from `old-ticket`,
   and without a state file — each a usage error, exit 2). The one exception
   is an earlier `mv` that died after writing the new state file but before
   removing the old one: if `tasks/<new>.json` names `<new>` and every repo
   of the old task is already at its destination, the rerun just finishes
   the rename. The new worktree directory may already exist only if
   it is empty or holds nothing but worktrees of this same task that an
   earlier, interrupted `mv` already moved there.
4. For each repo, moves its worktree from `<worktree_root>/old/<repo>` to
   `<worktree_root>/new/<repo>` via `git worktree move` — not a plain
   filesystem rename, so git's own worktree registration stays consistent.
   After each successful move the old ticket's state file is rewritten with
   that repo's new path, so state always matches what's on disk.
5. `mv` is **resumable**: if a move fails partway through a multi-repo task,
   it reports which repos moved and which didn't; fix the problem and rerun
   the same `mv old new` — repos already at their destination are skipped.
6. Once every repo has moved, writes `<state dir>/tasks/<new-ticket>.json`
   (with `ticket` updated to match) and removes the old state file. Anything
   else left in the old ticket workspace directory (notes, env files) is
   moved into the new one, and the old directory is removed only if that
   leaves it empty; entries that would collide are left in place with a
   warning.

Branch names are **not** renamed — they come from `branch_template` and
aren't necessarily derived from the ticket at all (see "Design notes"
above), so `TaskRepo.Branch` is left as-is.

### `fleet-task rm [ticket] [--force] [--json]`

The ticket is optional: if omitted, a ticket picker is shown. Refuses if the
ticket is currently active under `fleet-run`.

**Uncommitted-changes guard.** Before removing anything, every repo's
worktree is checked as in `show` (changes beyond fleet-task's own patches).
If any is `dirty` or `unknown`, the repos and their changed files (up to 10
each) are listed on stderr; on a terminal `rm` then asks `[y/N]` (default
no: exit 1, nothing removed); without one (or with `FLEET_NO_INPUT=1`) it is
a usage error (exit 2) naming `--force`, with nothing removed and, under
`--json`, no JSON. A worktree whose directory is already gone has nothing to
lose and doesn't trigger the guard. `--force` skips the check. Unpushed
commits never block: the branch is kept.

Then for each repo: runs `pre-remove` hooks, runs `git worktree remove
--force` (looking up the repo's `base` bare clone from `repos.yaml`; if the
repo entry is gone, it best-effort falls back to a plain `worktree remove`
and warns rather than aborting), then runs `post-remove` hooks. Two
self-healing cases: if git already deregistered the worktree but a leftover
directory remains (a dev server raced the delete), the leftovers are swept;
if the worktree directory was already deleted by hand, `rm` prunes git's stale
registration and counts it as removed instead of failing forever.

Once every repo is removed successfully, deletes the state file and then the
ticket's workspace directory **only if it is empty** — if it still holds
files (notes, env files), it's left in place and `rm` prints what remains. A
repo that fails to remove is kept in the state file (hooks still ran for it)
so a rerun of `rm` retries just that repo, and `rm` exits 1.

### Flags, help, and exit codes

Every subcommand has its own flag set and `--help` (`fleet-task rm --help`).
Flags and positional arguments may appear in either order, and `--` ends flag
parsing. Unknown flags are errors (`rm --json` is not read as a ticket).
`fleet-task -h|--help|help` prints the command list on stdout and exits 0;
`fleet-task version` / `--version` prints `fleet-task <version>` (set at
build time via `-ldflags "-X main.version=..."`, else the VCS revision Go
embedded in the binary).

Every ticket id accepted from the command line is validated, as a usage
error: at most 100 bytes; no path separators, not `.`/`..`; no leading `-`;
no whitespace, control characters or invalid UTF-8; none of `<>:"|?*` and no
trailing `.` or space (illegal in Windows filenames, which matter for
`runtime: windows` worktrees). Letters, digits and `-_.+@` etc. are fine.

`repos.yaml` is validated on load: repo names must be non-empty, unique, and
plain (no `/` or `\`, not `.`/`..`), and `runtime` must be empty, `linux` or
`windows`. `rm`/`mv` still work without a usable `repos.yaml` (warning if
it exists but is invalid).

Exit status is 0 on success, 1 on any operation failure, including *partial*
failure (`new`, `edit` and `rm` all attempt every repo before exiting 1 if
some failed), and **2 for a usage error or missing input**: bad flags,
unknown repo/patch names, an unknown ticket (`show`, `rm`, `edit`, `jump`,
`mv`: "no task found for ticket X"), an existing ticket for `new` or as
`mv`'s target, `mv` to the same name, an invalid ticket id or branch name,
uncommitted changes blocking `rm`/`edit --remove` without a terminal, or a
value that would need a prompt when prompting isn't allowed. Fatal errors print `error: ...`
and non-fatal ones `warning: ...` on stderr.

**No terminal, no prompts.** If stdin is not a terminal (a pipe, `/dev/null`)
or `FLEET_NO_INPUT=1` is set, no picker or prompt is ever opened — the
pickers draw on `/dev/tty`, so without this an agent could open one on its
user's terminal and hang. A missing value is an exit-2 error naming the flag
or argument that supplies it.

**`--json` on `new`, `edit`, `mv`, `rm`** prints one document on stdout
(progress, hook and git output move to stderr), also when the command then
exits 1:

```json
{"ticket": "PROJ-1", "renamed_from": "OLD-1", "ok": false,
 "repos": [{"repo": "api", "action": "created", "ok": true, "branch": "PROJ-1",
            "worktree_path": "...", "patches": ["local-env.patch"],
            "patch_errors": ["broken.patch: ..."]},
           {"repo": "ui", "action": "created", "ok": false, "error": "..."}]}
```

`action` is `created`, `removed`, `moved` or `skipped` (`mv` stops at the
first failure; later repos are `skipped`). `renamed_from` appears only for
`mv`, `patch_errors`/`error` only when non-empty. Usage errors (exit 2)
happen before any work and print no JSON.

### Hooks

Any executable file under `<config dir>/hooks/<phase>/*` runs (in
filename-sorted order) at the matching point in `new`/`edit`/`rm`, with
`FLEET_TICKET`, `FLEET_DESCRIPTION`, `FLEET_REPO`, `FLEET_BRANCH` and
`FLEET_WORKTREE_DIR` set in its environment.
Hook failures (non-zero exit) only print a warning to stderr — they never
abort `new`/`edit`/`rm`. See `../docs/CONTRACT.md` for the full contract.

| Phase          | Runs around                                          |
|----------------|-------------------------------------------------------|
| `pre-create`   | Before a repo's worktree is created (`new`, `edit`'s add-repo path) |
| `post-create`  | After a repo's worktree is created and patches applied (`new`, `edit`'s add-repo path) |
| `pre-remove`   | Before a repo's worktree is torn down (`rm`, `edit`'s remove-repo path) |
| `post-remove`  | After a repo's worktree is torn down (`rm`, `edit`'s remove-repo path) — `FLEET_WORKTREE_DIR` points to a path that's typically already gone from disk |

`pre-run`/`post-run` are run by `fleet-run` around per-window launches (see its README) —
`fleet-task` doesn't run them.

## Config & state file locations

All paths are XDG-compliant and overridable via `FLEET_*` env vars — see
`../docs/CONTRACT.md` for the authoritative table. Summary:

| Purpose             | Path                                          | Env override       |
|----------------------|------------------------------------------------|---------------------|
| Config dir           | `~/.config/fleet` (default)                     | `FLEET_CONFIG_DIR`  |
| Repos config file    | `<config dir>/repos.yaml`                       | `FLEET_REPOS_FILE`  |
| Patches dir          | `<config dir>/patches/<repo>/*.patch`           | -                   |
| Hooks dir            | `<config dir>/hooks/{pre-create,post-create,pre-remove,post-remove}/*` | -   |
| State dir            | `~/.local/state/fleet` (default)                | `FLEET_STATE_DIR`   |
| Per-task state file  | `<state dir>/tasks/<ticket>.json`               | -                   |
| Worktree root        | `<state dir>/worktrees` or `repos.yaml`'s `worktree_root` | -         |

## Design notes / deviations

- **`git apply` not `git am`**: patches are applied with `git apply`
  rather than `git am` for v1 simplicity. `git am` expects RFC 2822-style
  commit message headers (as produced by `git format-patch`) and can hang
  or fail on a plain `git diff`; `git apply` works uniformly for both at
  the cost of not preserving patch authorship/commit metadata. Good enough
  for "apply this working-tree change to get started."
- **Locking**: every mutating command (`new`, `edit`, `rm`, `mv`) holds the
  ticket's advisory lock for its whole run — taken before the state is read,
  released at exit — so two concurrent commands on one ticket can't lose each
  other's updates. Rather than pull in `golang.org/x/sys/unix` for a real
  `flock(2)`, the lock is a `<file>.lock` sibling created with `O_EXCL`
  holding the owner's pid; state writes go via a temp file + rename. It
  **fails fast** (exit 1, naming the holder's pid and the lock file) instead
  of waiting, since a holder may legitimately run for many minutes (`npm
  ci`). A lock whose pid is alive is never taken over, however old; a dead
  pid's lock is taken over automatically; a lock with no readable pid is
  taken over once older than a minute. Takeover renames the stale file to a
  unique name and re-checks it there, so two processes can't both "take
  over" and delete each other's fresh lock. `mv` locks the old and new
  ticket in sorted order. Per-ticket files make cross-ticket contention
  impossible, so this only matters for the same ticket. (If a pid is reused
  by an unrelated live process, the error says how to `rm` the lock.)
- **Selection UI**: `new` (repo/patch multiselect) and `jump` (worktree
  single-select) use an in-process [bubbletea](https://github.com/charmbracelet/bubbletea)
  picker (`select.go`, single entry point `runSelect(selectOpts{...})`; the
  ticket picker used by `rm`/`edit`/`mv` is `pickTicket` in `picker.go`) — vim movement
  keys, no external binary dependency (no `fzf` needed). `esc`/`q`/`ctrl-c` cancel (returns
  a non-nil error); a bare `enter` with nothing checked confirms whatever
  row the cursor is on.
- **Text input UI**: `new`'s Ticket/Description prompts are a single
  bubbletea form (`textinput.go`, `promptForm`) showing every field at once
  in its own bordered box rather than asking one `bufio.Reader` question at
  a time. `tab`/`shift+tab` or `up`/`down` move between fields in any order;
  within a field, `left`/`right`/`home`/`end` move the cursor and
  `backspace`/`delete` edit it. `enter` advances to the next field, or
  submits the form on the last one; `esc`/`ctrl-c` cancel.
- **Configurable branch names** (`branch_template`, `branch.go`): `repos.yaml`'s top-level
  `defaults.branch_template` and/or a per-repo `branch_template` override compute the git branch
  name for a new ticket, substituting `{ticket}` and `{description}` (the latter slugified:
  lowercased, non-alphanumeric runs collapsed to `-`, capped at 40 chars). Unset/empty is
  equivalent to `"{ticket}"` — today's plain-ticket-id behavior. Caveat: a template using
  `{description}` computes a different branch name if you rerun `fleet-task new` for the same
  ticket with a different description, so the "reattach to the existing branch" step won't find
  a match in that case and a new branch gets created instead — accepted as a tradeoff of allowing
  description-based names.
- **`fleet-task rm` without `repos.yaml` entry**: per the spec this is a
  "just try / skip with a warning" case; the fallback tries
  `git -C <worktree_path> worktree remove <worktree_path> --force`, which
  works because any worktree checkout has a `.git` file pointing back at
  its base repo. `mv` uses the same fallback.
- **Runtime split behind `gitRunner`** (`gitrunner.go`, `git.go`, `gitwin.go`):
  `runtime: linux` repos use WSL-native `git`; `runtime: windows` repos use
  `git.exe` via `powershell.exe` against a worktree on the NTFS side, with a
  WSL-side symlink at the normal worktree path. `new`/`rm`/`mv` all go through
  the same `gitRunner` interface instead of per-runtime switches. Before
  any work, the Windows runner's `Preflight` checks that `windows_base` and the
  real worktree location share a drive (NTFS hardlinks can't cross drives; the
  cache-root side of that check is `fleet-cache`'s). The
  PowerShell wrappers (`winpath.go`) run under `$ErrorActionPreference='Stop'`
  with try/catch so cmdlet failures (e.g. `New-Item`) yield a non-zero exit
  rather than being masked by `exit $LASTEXITCODE`. `Preflight`'s same-drive
  check is deterministic and rejects a path with no drive letter (UNC) by
  name. The WSL-side worktree symlink is only ever replaced if what's there is
  a symlink; a real file or directory at that path is an error, never deleted.
- **`FLEET_TMUX_SOCKET`**: if set, `fleet-task`'s read of
  `@fleet_task_ticket` runs `tmux -L <value>`, matching `fleet-run`, so tests
  can use an isolated tmux server.

## Testing

Pure-logic pieces are covered with Go unit tests; git-dependent behaviour is
tested against real temp repos (`t.TempDir()`, `GIT_CONFIG_GLOBAL=/dev/null`,
fixed author env; skipped if `git` is missing):

- `git_test.go` — base setup (fresh, idempotent, completing an interrupted
  init, refusing a foreign directory, `fetch --prune`), worktree add modes
  (attach local / track pushed branch / create from default with no
  upstream / typo'd default), branch-name validation.
- `e2e_test.go` — `new` writing state after each repo (observed from a hook,
  which also checks the hook env), `mv` finishing an interrupted rename.
- `changes_test.go` — the uncommitted-changes check (patches excluded, edits
  inside/outside patched files, untracked, ignored, deleted or committed
  patch, unknown patches, missing worktree) and the `rm`/`edit --remove`
  guard with and without `--force`.
- `gitrunner_test.go` — the base-setup and worktree-add planners.
- `misc_test.go` — hook env, `FLEET_TMUX_SOCKET`, version resolution,
  headless git env, `replaceSymlink`, exit codes for unknown tickets/`mv`,
  `--help`/`version` on stdout.

- `config_test.go` — `repos.yaml` parsing against the contract's example.
- `state_test.go` — per-task JSON state file round-trip, and the
  `tasks/*.json` globbing/listing logic against temp-dir fixtures.
- `lock_test.go` — lock-file pid parsing, stale-lock rules (live pid never
  stale), takeover (including restoring a live lock grabbed in a race),
  fail-fast error text, release only by the owner.
- `hooks_test.go` — hook discovery: executables only, filename-sorted,
  directories/non-executables skipped, missing dir tolerated.
- `ssh_test.go` — SSH URL detection (`git@...` / `ssh://...` vs `https://...`).
- `args_test.go` — per-subcommand flag parsing: interspersed flags/positionals,
  `--`, unknown flags, `--help`.
- `common_test.go` — ticket-id validation, unknown-ticket usage errors,
  per-ticket locking around reads.
- `cmd_jump_test.go` — flattening tasks into jump rows.
- `cmd_edit_test.go` — which repos are offered by `edit`'s add picker.
- `cmd_mv_test.go` — ticket-id validation, moving leftovers out of the old
  ticket dir, and resumable-`mv` checks (`alreadyMoved`, `checkNewTicketDir`).
- `cmd_rm_test.go` — `selfHealWorktreeRemove` (leftover sweep, already-missing
  worktree) and empty-only ticket dir removal.
- `select_test.go` — the picker's state machine (`selectModel.Update`):
  vim navigation, toggling, filtering, preselection, cancel — driven
  directly with synthetic `tea.KeyMsg`s, no real terminal needed.
- `textinput_test.go` — the Ticket/Description form's state machine (focus
  movement, cursor editing, submit/cancel).
- `winpath_test.go` — the PowerShell script builders and `windowsGit` path
  derivation (not the powershell.exe invocation itself).
- `branch_test.go` — `branch_template` substitution and description
  slugification.

The Windows runner (`gitwin.go`, PowerShell/`git.exe`), `ssh-add` and
`fleet-cache` calls aren't exercised by the tests (only the PowerShell script
builders and parsers are) — they need WSL interop and are meant to be tested
manually. The picker's *rendering* and
actual terminal wiring (`select.go`'s `runSelect`/`tea.NewProgram`) is
similarly left to manual testing — there's no headless way to drive a real interactive TUI short
of a full PTY harness, which isn't worth the weight here.

Build and test:

```sh
go build ./...
go test ./...
```
