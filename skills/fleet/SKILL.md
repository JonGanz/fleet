---
name: fleet
description: Drive the fleet CLIs (fleet-task, fleet-run) to work a ticket or issue (e.g. a Jira key like PROJ-123, a GitHub issue like gh-123, or any task name) in isolated git worktrees and run its apps in tmux. fleet has no issue-tracker integration — it never reads or updates tickets. Use when asked to start, set up, check out, switch to, run, test, inspect logs of, or tear down work for a ticket managed by fleet, or when a task mentions fleet-task / fleet-run / worktrees per ticket.
---

# fleet

`fleet-task` creates one git worktree per repo per ticket. `fleet-run` runs a
ticket's apps, one tmux window per `repo:run-name` target. Both are on `PATH`.

## Ground rules

- **Always set `FLEET_NO_INPUT=1`** on every command (e.g.
  `export FLEET_NO_INPUT=1` once per shell). With it, no picker can ever open
  on the user's terminal; a missing value fails fast with exit 2 and names
  the flag to add.
- **Always pass `--json`** where supported (everything except
  `fleet-task jump`, `fleet-run attach` and `version`) and parse stdout.
  Progress, git and hook output go to stderr; the tools' own errors and
  warnings there start with `<tool>: error:` / `<tool>: warning:`.
- **Exit codes:** 0 ok · 1 operation failed (maybe partly; read the JSON) ·
  2 usage error / missing input (nothing was changed; fix the arguments).
- **Never `fleet-run attach`**: it hands a terminal to tmux and is for the
  user only (it exits 2 without one). Use `status` and `logs` instead.
- **Only one ticket runs at a time.** `fleet-run start` for a different
  ticket than the active one **kills every running window first**. Check
  `fleet-run status --json` before starting; if another ticket is active and
  the user didn't ask to switch, ask before starting.
- **Never `rm`/`edit --remove`/`mv` a ticket that is active** — the tool
  refuses; run `fleet-run stop --all` first, and only if the user is done
  with those apps.
- **Never pass `--force` to `rm` / `edit --remove` on your own.** It skips
  the uncommitted-changes check (and `--delete-branch`'s confirmation) and
  deletes that work. Use it only after the user saw what would be lost and
  agreed to discard it. The same goes for `--delete-branch` and for
  `fleet-task unlock --force`.
- **Patches are local dev tweaks, never commit them.** They show up as
  uncommitted changes in the worktree. Stage files explicitly; never
  `git add -A` / `git commit -a` in a fleet worktree.
- Don't invent ticket ids, repo names or patch names: discover them.

## Ticket ids

A ticket id is a short name usable as a file name: no `/` or `\`, no
whitespace, none of `<>:"|?*`, not `.`/`..`, no leading `-`, no trailing
`.`, at most 100 bytes (an invalid one exits 2). fleet never contacts an
issue tracker. Use the tracker's own key when there is one —
`PROJ-123` (Jira), `gh-123` (GitHub issue #123; avoid a bare `#`, which the
shell treats as a comment), `ENG-42` (Linear) — or a short name. Reuse the
exact id of an existing task (`fleet-task list --json`). Reading ticket
details or changing status (in progress / in review) is done with that
tracker's own tool (e.g. `gh issue view`, a Jira connector), never fleet.

## Discover

```sh
fleet-task list --json              # existing tasks
fleet-task list --status --json     # + dirty_repos/unknown_repos per task (slower)
fleet-task show <ticket> --json     # one task: repos, branch, worktree_path, patches, changes, unpushed_commits, active
fleet-task repos --json             # configured repos: runtime, default_branch, run targets, patches
fleet-task patches <repo> --json    # {name, summary, files} per patch
fleet-run targets --ticket <ticket> --json
fleet-run status --json             # active ticket, stale_active, windows, per-pane alive/exit status
```

`show --json` reports per repo `changes`: `{state, files[{status, path}],
patches_excluded, approximate, reason?}`. `state` is `clean`, `dirty` or
`unknown`; changes from fleet's own applied patches are not counted. If
`patches_excluded` is false (patch list unknown, patch gone or no longer
applying), patch edits count as changes too — `reason` says why.
`approximate` (runtime: windows) means edits inside patched files aren't
seen. `unpushed_commits` (or `null`): `rm` keeps a branch with unpushed
commits (it deletes only branches whose commits are all on a remote).

In `fleet-run targets --json` use `target` (`repo:run-name`); `label` is the
same value, deprecated.

## Create a ticket's worktrees

1. Pick repos from `fleet-task repos --json` based on what the ticket
   touches. Ask the user if unclear.
2. For each repo, read `fleet-task patches <repo> --json`. All patches are
   applied by default (the user's own default). Skip one only if its `files`
   overlap what the ticket will change, or the user said to.
3. Create:
   ```sh
   fleet-task new <TICKET> -d "<short description>" --repo api --repo admin-ui \
     [--skip-patch api:local-env.patch | --no-patches] --json
   ```
   - Exit 2 "already exists" → the task exists: use `show`, or `edit --add`.
     This is also how to finish an interrupted `new` (killed mid-run): the
     state file lists every repo created so far, so `show` it and
     `edit --add` the missing repos (if there is no state file, rerun
     `new`). A worktree the killed run had already created on the right
     branch is adopted (`"adopted": true`, `patches: null` — its patches
     were not re-applied, so check with `show` and tell the user). If the
     path holds something else, the error says what; tell the user rather
     than deleting it.
   - Per repo, check `ok`, `error`, and `patch_errors` (a patch that failed
     to apply is not fatal; tell the user). An `error` naming a
     `pre-create hook` means the user's hook refused that repo: report the
     hook and its exit status; don't work around it.
   - The description feeds branch templates; keep it short.
4. Work in each repo's `worktree_path` (from the result or `show`). Get one
   path with `fleet-task jump <TICKET> --repo <repo>`.

Add or remove repos later:
```sh
fleet-task edit <TICKET> --add db --remove admin-ui [--skip-patch db:x.patch] --json
```

## Run and check apps

```sh
fleet-run status --json                         # who's active?
fleet-run targets --ticket <TICKET> --json      # what can run?
fleet-run start --ticket <TICKET> api:dev admin-ui:dev --json   # or --all
fleet-run restart --ticket <TICKET> api:dev --json  # restart even if running
fleet-run logs api:dev --lines 200 --json       # read output
fleet-run stop --ticket <TICKET> api:dev --json # or --all
```

- After `start`, poll `logs` for a readiness line (e.g. "listening on")
  with a short sleep between checks rather than assuming it's up.
- A crashed app stays as a dead pane: `status` shows `alive: false` with
  `exit_status`, and `logs` still returns its output. Fix the cause, then
  `start` the same target again to respawn it.
- Exception: `runtime: windows` targets run inside `powershell.exe -NoExit`,
  which stays open after the app dies — their panes always look alive, so
  read `logs` to tell whether the app is still running.
- `targets` reports `running: true` only for the active ticket's live panes.
- `stop --ticket X` only asserts X is active (it errors out otherwise); it is
  not a filter.
- Use `restart` after changing config or code the app doesn't hot-reload;
  `start` leaves a running app alone.
- **Stale active ticket:** `status` shows `stale_active: true` when a ticket
  is recorded active but no windows exist (the session was killed outside
  fleet-run). `fleet-task rm` refuses that ticket until it's cleared;
  `fleet-run stop --ticket <TICKET> --json` clears it
  (`cleared_active: true`) and touches nothing else, since nothing is
  running.

## Rename / tear down

```sh
fleet-task mv <OLD> <NEW> --json    # rerunnable after a partial failure
fleet-task rm <TICKET> --json       # stop its apps first
```
Only remove a task when the user asks. First run `fleet-task show <TICKET>
--json` and report any repo whose `changes.state` isn't `clean` (with its
files) and any `unpushed_commits`. `rm` and `edit --remove` refuse a dirty
or `unknown` worktree: without a terminal they exit 2 naming `--force`,
nothing removed. Then either the user commits/discards the changes, or —
only if they explicitly agree to lose them — rerun with `--force`.

After removing a worktree, `rm` deletes its local branch only if every commit
on it is on a remote; otherwise it keeps it (`branch_deleted: false`,
`branch_kept_reason` in the JSON) — tell the user which branches were kept
and why. `--keep-branch` always keeps it. `--delete-branch` deletes even
unpushed work: only with the user's explicit agreement (without a terminal
it exits 2 unless `--force` is also given). A `pre-remove` hook can refuse a
repo (`error` names it; the repo stays in the task).

## Failure handling

- Exit 2: read the message, it names the missing/invalid flag; fix and retry.
  Also exit 2: an unknown ticket ("no task found" / "no task state found";
  check `fleet-task list`), an invalid ticket id or branch name, and
  uncommitted changes blocking `rm`/`edit --remove` (see above).
- Exit 1 with JSON: retry only the failed repos (`edit --add <repo>` for a
  failed create; rerun `rm`/`mv` — both resume). A `pre-*` hook failure or a
  timeout ("killed after the … timeout": a hung hook, or a fetch exceeding
  `git.network_timeout`) is in `error`; retrying the same thing won't help —
  tell the user.
- "task is locked by another fleet-task" (exit 1): another fleet-task
  command is working on the same ticket (it may take minutes, e.g. npm).
  Wait and retry. If it persists, run `fleet-task unlock <TICKET> --json`: it
  removes the lock only if its owner process is gone, and refuses (exit 1,
  naming the pid) while it is still running. Never pass `--force` to
  `unlock` unless the user agreed, and never delete the `.lock` file
  yourself.
- A fetch failing with ssh's `Permission denied` or `Host key verification
  failed`: without a terminal fleet runs ssh in BatchMode, so it can't ask
  for a passphrase or confirm a host key. Tell the user (e.g. load the key
  into ssh-agent); don't change their ssh config.
- "currently active in fleet-run": stop the apps (with the user's OK) first.
- "has schema_version N, but this fleet only understands up to M": the state
  file was written by a newer fleet. Don't edit or delete it; tell the user to
  upgrade fleet. (`fleet-task list` skips such a ticket with a warning.)
