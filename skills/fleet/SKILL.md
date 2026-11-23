---
name: fleet
description: Drive the fleet CLIs (fleet-task, fleet-run) to work a Jira ticket in isolated git worktrees and run its apps in tmux. Use when asked to start, set up, check out, switch to, run, test, inspect logs of, or tear down work for a ticket managed by fleet, or when a task mentions fleet-task / fleet-run / worktrees per ticket.
---

# fleet

`fleet-task` creates one git worktree per repo per ticket. `fleet-run` runs a
ticket's apps, one tmux window per `repo:run-name` target. Both are on `PATH`.

## Ground rules

- **Always set `FLEET_NO_INPUT=1`** on every command (e.g.
  `export FLEET_NO_INPUT=1` once per shell). With it, no picker can ever open
  on the user's terminal; a missing value fails fast with exit 2 and names
  the flag to add.
- **Always pass `--json`** where supported and parse stdout. Progress, git
  and hook output go to stderr.
- **Exit codes:** 0 ok · 1 operation failed (maybe partly; read the JSON) ·
  2 usage error / missing input (nothing was changed; fix the arguments).
- **Only one ticket runs at a time.** `fleet-run start` for a different
  ticket than the active one **kills every running window first**. Check
  `fleet-run status --json` before starting; if another ticket is active and
  the user didn't ask to switch, ask before starting.
- **Never `rm`/`edit --remove`/`mv` a ticket that is active** — the tool
  refuses; run `fleet-run stop --all` first, and only if the user is done
  with those apps.
- **Patches are local dev tweaks, never commit them.** They show up as
  uncommitted changes in the worktree. Stage files explicitly; never
  `git add -A` / `git commit -a` in a fleet worktree.
- Don't invent ticket ids, repo names or patch names: discover them.

## Discover

```sh
fleet-task list --json              # existing tasks
fleet-task show <ticket> --json     # one task: repos, branch, worktree_path, patches, active
fleet-task repos --json             # configured repos: runtime, default_branch, run targets, patches
fleet-task patches <repo> --json    # {name, summary, files} per patch
fleet-run targets --ticket <ticket> --json
fleet-run status --json             # active ticket, windows, per-pane alive/exit status
```

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
   - Per repo, check `ok`, `error`, and `patch_errors` (a patch that failed
     to apply is not fatal; tell the user).
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

## Rename / tear down

```sh
fleet-task mv <OLD> <NEW> --json    # rerunnable after a partial failure
fleet-task rm <TICKET> --json       # stop its apps first
```
Only remove a task when the user asks: `rm` deletes the worktrees, including
uncommitted work. Check `git status` in each worktree first and report
anything uncommitted (besides patch changes) before removing.

## Failure handling

- Exit 2: read the message, it names the missing/invalid flag; fix and retry.
- Exit 1 with JSON: retry only the failed repos (`edit --add <repo>` for a
  failed create; rerun `rm`/`mv` — both resume).
- "currently active in fleet-run": stop the apps (with the user's OK) first.
