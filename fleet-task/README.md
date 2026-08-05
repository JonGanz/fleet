# fleet-task

`fleet-task` creates and manages per-Jira-ticket sets of git worktrees, one
per repo the ticket touches, so several tickets (and their AI-agent
pipelines) can be worked in parallel without their working trees
conflicting. It's one of three independent CLIs (`fleet-task`, `fleet-run`,
`fleet-cache`) in the fleet suite; see `../docs/CONTRACT.md` for the full
shared contract (paths, `repos.yaml` schema, state file format, hook
conventions).

## Commands

### `fleet-task new`

Interactively:

1. Prompts for a ticket id and description.
2. Loads `repos.yaml` and lets you multiselect which repos this task
   touches, via an in-process picker (vim keys: `j`/`k` move, `gg`/`G` top/
   bottom, `space` toggle, `a` select all, `A` clear, `/` filter, `enter`
   confirm, `esc`/`q` cancel).
3. For each selected repo with patch files under
   `<config dir>/patches/<repo>/*.patch`, lets you multiselect which to
   apply — every patch starts checked, so you uncheck the ones you don't
   want rather than checking the ones you do (skipped entirely if the
   repo has no patches). All repos' patch prompts happen up front, before
   any directory/worktree work starts for any of them, so the whole
   interactive part of `new` front-loads instead of interleaving prompts
   with git/hook/cache work.
4. For each selected repo, in order:
   - Ensures the repo's shared bare clone (`base` in `repos.yaml`) exists,
     cloning it on first use or fetching `origin` otherwise. If `origin`
     looks like an SSH URL, runs `ssh-add -l` first and warns (but doesn't
     block) if no identity looks loaded.
   - Runs `pre-create` hooks.
   - Creates a worktree at
     `<worktree_root>/<ticket>/<repo>` on a branch named after the ticket,
     branching off `origin/<default_branch>`. Reruns for the same ticket
     fall back to attaching the existing branch instead of failing.
   - Applies the selected patches with `git apply` (see "Design notes"
     below for why `apply` rather than `am`).
   - Runs `post-create` hooks.
   - If the new worktree has a `package-lock.json`, shells out to
     `fleet-cache ensure <worktree_path>` (must be on `PATH`); a cache
     failure warns but doesn't abort the rest of `new`.
5. Writes `<state dir>/tasks/<ticket>.json` describing the task and its
   repos/worktrees.

### `fleet-task list [--json]`

Globs `<state dir>/tasks/*.json` and prints a table of
ticket/description/repo-count/created-at, or the raw JSON array with
`--json`.

### `fleet-task jump`

Globs `<state dir>/tasks/*.json`, flattens all (ticket, repo, worktree
path) rows across every task, and lets you pick one via the same
in-process picker as `new` (single-select: no checkboxes, `enter`
confirms the highlighted row). Prints *only* the chosen worktree path to
stdout (nothing else), so it composes with a shell wrapper (a subprocess
can't `cd` its parent shell). If selection is cancelled (e.g. `Esc`/`q`),
exits non-zero with no stdout output.

See `contrib/fleet.sh` for the `fj` shell function that does the `cd` for
you:

```bash
source /path/to/fleet-task/contrib/fleet.sh
fj   # select a worktree, cd into it
```

### `fleet-task rm <ticket>`

Loads that ticket's state file, runs `git worktree remove --force` for
each repo (looking up the repo's `base` bare clone from `repos.yaml`; if
the repo entry is gone, it best-effort falls back to a plain
`worktree remove` and warns rather than aborting), then deletes the state
file.

## Config & state file locations

All paths are XDG-compliant and overridable via `FLEET_*` env vars — see
`../docs/CONTRACT.md` for the authoritative table. Summary:

| Purpose             | Path                                          | Env override       |
|----------------------|------------------------------------------------|---------------------|
| Config dir           | `~/.config/fleet` (default)                     | `FLEET_CONFIG_DIR`  |
| Repos config file    | `<config dir>/repos.yaml`                       | `FLEET_REPOS_FILE`  |
| Patches dir          | `<config dir>/patches/<repo>/*.patch`           | -                   |
| Hooks dir            | `<config dir>/hooks/{pre-create,post-create}/*` | -                   |
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
- **Locking**: the contract asks for an flock-style safety belt on
  per-task state file writes. Rather than pull in `golang.org/x/sys/unix`
  for a real `flock(2)`, `writeTaskState` takes an advisory lock by
  creating a `<file>.lock` sibling with `O_EXCL` (spin-retry up to 5s),
  then writes via a temp file + rename. This is portable, dependency-free,
  and sufficient given per-ticket files make cross-ticket contention
  impossible by construction — the lock only matters for the rare case of
  two invocations racing on the *same* ticket.
- **Selection UI**: `new` (repo/patch multiselect) and `jump` (worktree
  single-select) use an in-process [bubbletea](https://github.com/charmbracelet/bubbletea)
  picker (`select.go`) instead of shelling out to `fzf` — vim movement
  keys, no external binary dependency. `esc`/`q`/`ctrl-c` cancel (returns
  a non-nil error); a bare `enter` with nothing checked confirms whatever
  row the cursor is on, matching fzf's old behavior.
- **`fleet-task rm` without `repos.yaml` entry**: per the spec this is a
  "just try / skip with a warning" case; the fallback tries
  `git -C <worktree_path> worktree remove <worktree_path> --force`, which
  works because any worktree checkout has a `.git` file pointing back at
  its base repo.

## Testing

Pure-logic pieces are covered with Go unit tests (no real git/ssh/npm
needed):

- `config_test.go` — `repos.yaml` parsing against the contract's example.
- `state_test.go` — per-task JSON state file round-trip, and the
  `tasks/*.json` globbing/listing logic against temp-dir fixtures.
- `hooks_test.go` — hook discovery: executables only, filename-sorted,
  directories/non-executables skipped, missing dir tolerated.
- `ssh_test.go` — SSH URL detection (`git@...` / `ssh://...` vs `https://...`).
- `cmd_jump_test.go` — flattening tasks into jump rows.
- `select_test.go` — the picker's state machine (`selectModel.Update`):
  vim navigation, toggling, filtering, preselection, cancel — driven
  directly with synthetic `tea.KeyMsg`s, no real terminal needed.

Subprocess-driving code (git commands, ssh-add, fleet-cache) is factored
into small, separately-named functions (`git.go`, `cache.go`) that aren't
exercised by the unit tests — they need real binaries/repos and are meant
to be tested manually / in integration. The picker's *rendering* and
actual terminal wiring (`select.go`'s `runSelect`/`tea.NewProgram`) is
similarly left to manual testing, same as before with fzf's subprocess
plumbing — there's no headless way to drive a real interactive TUI short
of a full PTY harness, which isn't worth the weight here.

Build and test:

```sh
go build ./...
go test ./...
```
