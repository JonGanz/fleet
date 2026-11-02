# fleet-task

`fleet-task` creates and manages per-Jira-ticket sets of git worktrees (extra
working directories sharing one git history), one
per repo the ticket touches, so several tickets (and their AI-agent
pipelines) can be worked in parallel without their working trees
conflicting. It's one of three independent CLIs (`fleet-task`, `fleet-run`,
`fleet-cache`) in the fleet suite; see `../docs/CONTRACT.md` for the full
shared contract (paths, `repos.yaml` schema, state file format, hook
conventions).

## Commands

### `fleet-task new`

Interactively:

1. Prompts for a ticket id and description. The ticket id is validated (no
   path separators, not `.`/`..`), and `new` **refuses a ticket that already
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
     history but no checked-out files) exists,
     cloning it on first use or fetching `origin` otherwise. If `origin`
     looks like an SSH URL, runs `ssh-add -l` before cloning and warns (but
     doesn't block) if no identity looks loaded.
   - Creates a worktree at `<worktree_root>/<ticket>/<repo>` on a branch
     computed from `branch_template` (repos.yaml, per-repo or fleet-wide
     default — see "Design notes" below; defaults to the plain ticket id if
     unset), branching off `origin/<default_branch>`. If creating the branch
     fails (e.g. it already exists), it falls back to attaching the existing
     branch; if both attempts fail, the error includes git's message for
     each (so a typo'd `default_branch` is visible rather than a bare
     "exit status 128").
   - Applies the selected patches with `git apply` (see "Design notes"
     below for why `apply` rather than `am`).
   - Runs `post-create` hooks.
   - If the new worktree has a `package-lock.json`, shells out to
     `fleet-cache ensure <worktree_path>` (must be on `PATH`); a cache
     failure warns but doesn't abort the rest of `new`.
5. Writes `<state dir>/tasks/<ticket>.json` describing the task and its
   repos/worktrees (the repos that succeeded). If some repos failed, every
   repo is still attempted and recorded where possible, and `new` exits 1
   at the end; add the failed ones later with `fleet-task edit`.

### `fleet-task list [--json]`

Globs `<state dir>/tasks/*.json` and prints a table of
ticket/description/repo-count/created-at, or the raw JSON array with
`--json`.

### `fleet-task jump [ticket]`

Globs `<state dir>/tasks/*.json`, flattens all (ticket, repo, worktree
path) rows across every task, and lets you pick one via the same
in-process picker as `new` (single-select: no checkboxes, `enter`
confirms the highlighted row). Prints *only* the chosen worktree path to
stdout (nothing else), so it composes with a shell wrapper (a subprocess
can't `cd` its parent shell). With a ticket argument, only that ticket's repos
are offered. If selection is cancelled (e.g. `Esc`/`q`),
exits non-zero with no stdout output.

See `contrib/fleet.sh` for the `fj` shell function that does the `cd` for
you:

```bash
source /path/to/fleet-task/contrib/fleet.sh
fj   # select a worktree, cd into it
```

### `fleet-task edit [ticket]`

Lets an already-created task's repo list grow or shrink, without tearing
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

`edit` refuses to *remove* repos while the ticket is the one currently active
in `fleet-run` (read from the `@fleet_task_ticket` tmux option), with a message
telling you to `fleet-run stop` first; adding repos is unaffected. If any
add or remove failed, `edit` still attempts everything and saves the state,
then exits 1. If nothing is selected it prints "no changes selected" on
stdout and exits 0.

### `fleet-task mv [old-ticket] [new-ticket]`

Renames a ticket in place, without tearing down and recreating its
worktrees:

1. If `old-ticket` is omitted, shows the same interactive ticket picker as
   `rm`. If `new-ticket` is omitted, prompts for it.
2. Refuses if `old-ticket` is currently active under `fleet-run` (same guard
   `rm`/`edit` use) — renaming a live ticket would desync `fleet-run`'s tmux
   state from the new filename.
3. Validates `new-ticket` (non-empty, no path separators, doesn't already
   have a state file). The new worktree directory may already exist only if
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

### `fleet-task rm [ticket]`

The ticket is optional: if omitted, a ticket picker is shown. Refuses if the
ticket is currently active under `fleet-run`. Loads that ticket's state file,
then for each repo: runs `pre-remove` hooks, runs `git worktree remove
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
Every ticket id accepted from the command line is validated (no path
separators, not `.`/`..`).

Exit status is 0 on success and 1 on any failure, including *partial*
failure: `new`, `edit` and `rm` all attempt every repo before exiting 1 if
some failed. Fatal errors print `error: ...` and non-fatal ones
`warning: ...` on stderr.

### Hooks

Any executable file under `<config dir>/hooks/<phase>/*` runs (in
filename-sorted order) at the matching point in `new`/`edit`/`rm`, with
`FLEET_TICKET`/`FLEET_REPO`/`FLEET_WORKTREE_DIR` set in its environment.
Hook failures (non-zero exit) only print a warning to stderr — they never
abort `new`/`edit`/`rm`. See `../docs/CONTRACT.md` for the full contract.

| Phase          | Runs around                                          |
|----------------|-------------------------------------------------------|
| `pre-create`   | Before a repo's worktree is created (`new`, `edit`'s add-repo path) |
| `post-create`  | After a repo's worktree is created and patches applied (`new`, `edit`'s add-repo path) |
| `pre-remove`   | Before a repo's worktree is torn down (`rm`, `edit`'s remove-repo path) |
| `post-remove`  | After a repo's worktree is torn down (`rm`, `edit`'s remove-repo path) — `FLEET_WORKTREE_DIR` points to a path that's typically already gone from disk |

`pre-run`/`post-run` also exist in the contract, reserved for `fleet-run`
around per-window launches — `fleet-task` doesn't run them.

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
- **Locking**: the contract calls for an advisory-lock safety belt on
  per-task state file writes. Rather than pull in `golang.org/x/sys/unix`
  for a real `flock(2)`, `writeTaskState` takes an advisory lock by
  creating a `<file>.lock` sibling with `O_EXCL` (spin-retry up to 5s),
  then writes via a temp file + rename. The lock file records the owner's
  pid; a lock left behind by a crashed run (Ctrl-C mid-write) is detected —
  owner pid no longer alive, or the file older than 5 minutes — and taken
  over automatically. If a lock that looks live never frees up, the timeout
  error names the lock file and says to `rm` it. This is portable, dependency-free,
  and sufficient given per-ticket files make cross-ticket contention
  impossible by construction — the lock only matters for the rare case of
  two invocations racing on the *same* ticket.
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
  ticket with a different description, so the "reattach to the existing branch" fallback in
  `setupWorktree` won't find a match in that case and a new branch gets created instead — accepted
  as a tradeoff of allowing description-based names.
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
  rather than being masked by `exit $LASTEXITCODE`.

## Testing

Pure-logic pieces are covered with Go unit tests (no real git/ssh/npm
needed):

- `config_test.go` — `repos.yaml` parsing against the contract's example.
- `state_test.go` — per-task JSON state file round-trip, and the
  `tasks/*.json` globbing/listing logic against temp-dir fixtures.
- `lock_test.go` — lock-file pid parsing, stale-lock detection and takeover,
  timeout error text.
- `hooks_test.go` — hook discovery: executables only, filename-sorted,
  directories/non-executables skipped, missing dir tolerated.
- `ssh_test.go` — SSH URL detection (`git@...` / `ssh://...` vs `https://...`).
- `args_test.go` — per-subcommand flag parsing: interspersed flags/positionals,
  `--`, unknown flags, `--help`.
- `common_test.go` — ticket-id validation in `taskFile`, worktree-add fallback
  error reporting.
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

Subprocess-driving code (git commands, ssh-add, fleet-cache) is factored
into small, separately-named functions (`git.go`, `gitwin.go`, `cache.go`) that aren't
exercised by the unit tests — they need real binaries/repos and are meant
to be tested manually / in integration. The picker's *rendering* and
actual terminal wiring (`select.go`'s `runSelect`/`tea.NewProgram`) is
similarly left to manual testing — there's no headless way to drive a real interactive TUI short
of a full PTY harness, which isn't worth the weight here.

Build and test:

```sh
go build ./...
go test ./...
```
