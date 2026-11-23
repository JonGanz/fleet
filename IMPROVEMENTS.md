# Improvements backlog

Decisions in progress, feature ideas not started, paths never exercised end to end, and deliberate
decisions.

## Decided, in progress
- **Cache key:** sha256 of `package-lock.json` plus the `.npmrc` contents (no Node version/arch).
  Contract change; existing entries become unreferenced and are rebuilt once, `gc` removes the old
  ones.
- **Hooks:** a non-zero exit from a `pre-create`, `pre-remove` or `pre-run` hook cancels that
  repo/window (reported as failed, the rest still attempted); `post-*` hooks still only warn. Every
  hook is killed after a timeout, default 5 minutes, configurable in `repos.yaml`.
- **Hangs:** network git operations (fetch, the first fetch of a new base) get a timeout, default
  10 minutes, configurable. When no terminal is available and neither `GIT_SSH_COMMAND` nor
  `core.sshCommand` is set, fleet sets `GIT_SSH_COMMAND="ssh -o BatchMode=yes"`; the user's own SSH
  setting is never overridden.
- **Branch cleanup:** `rm` deletes the local ticket branch only when every commit on it is on a
  remote, otherwise keeps it and says so; `--keep-branch` always keeps it; `--delete-branch` forces
  deletion behind the same confirmation as `--force`.
- **Interrupted `new`:** when `new` / `edit --add` finds the target path is already a worktree of
  that repo's base on the expected branch, adopt it (run the remaining steps: hooks and
  `node_modules`) instead of failing; anything else at the path stays an error.
- **Lock:** add `fleet-task unlock <ticket>`, removing a ticket's lock only when the owning process
  is gone (refuse while it is alive; `--force` overrides). The "task is locked" error and the agent
  skill point to it instead of a raw `rm`.
- **Function order:** sort functions alphabetically within every file (methods grouped under their
  type), as a dedicated pass after the restructure; no CI enforcement.
- **CI:** pin staticcheck to v0.8.2; govulncheck stays at `@latest` on purpose (its job is to
  surface new vulnerabilities).

## Feature ideas (not started)
- `fleet-task doctor` / `validate`: dependencies, config, orphans (worktree without state, state
  without worktree — e.g. the repo a killed `new` was working on when interrupted).
- `fleet-task sync` (fetch + rebase onto the default branch) and `new --base <ref>`.
- `fleet-task init` (scaffold `repos.yaml`) and `config` (print resolved paths).
- `fleet-run logs --follow`, and persistent logs via `pipe-pane` (logs are capped by tmux
  `history-limit` today).
- Per-run `env` and `cwd` subdirectory in `repos.yaml`.
- fleet-cache: pnpm/yarn/bun, nested lockfiles, npm workspaces.
- A `refresh` that re-runs `ensure` after a lockfile change.
- `gc` with automatic roots (e.g. a `fleet-task gc` wrapper passing the worktree root).
- Shell completions and man pages.
- Stamp the version into the installed skill.
- `fleet-task rm --dry-run` (and `edit --remove --dry-run`): show what would be removed, including
  the uncommitted-changes check, without removing anything.
- Signal handling in fleet-task and fleet-run: only fleet-cache handles SIGINT/SIGTERM today. A
  Ctrl-C during `new`/`edit`/`rm`/`mv` or a hook stops the process mid-step; state is persisted per
  repo, and the adoption decision above covers the leftover worktree, but child processes (git,
  npm via fleet-cache, hooks) are not stopped cleanly and the lock is left for pid-based takeover.
- Lint configuration file (`staticcheck.conf` or a golangci config) so `make lint` and CI use the
  same, explicit checks.
- Release tooling, deferred until the tool has other users: GoReleaser (linux amd64/arm64 +
  darwin), GitHub Releases, optional Homebrew tap / deb, and a CHANGELOG generated from the
  conventional commits (git-cliff or release-please).

## Untested paths (never run end to end)
- Everything `runtime: windows`: `windowsGit` and symlinked worktrees, `fleet-cache
  ensure-windows` / `gc-windows` (only helpers and generated PowerShell scripts were exercised),
  the approximate changes check, PowerShell quoting, interrupt behaviour of Windows-side npm, and
  the staging-to-entry rename (a plain `os.Rename` over `/mnt/c`) under antivirus/indexer
  contention.
- fleet-run `remain-on-exit`, `logs` and dead-pane respawn on `runtime: windows` targets
  (`powershell.exe -NoExit` never exits on its own, so a crash there shows as a live pane).
- `fleet-run attach` and the interactive pickers (no tty in tests; only the pickers' state
  machines are unit tested).
- The CI workflow and dependabot config: never run on GitHub, only parsed as YAML.
- shellcheck and staticcheck were never run locally (not installed).

## Decisions kept as-is (revisit only if wanted)
- `--force` means "rebuild" on `ensure`, "really delete" on `gc`, and "skip the
  uncommitted-changes check" on `rm` / `edit`. Help text clarified only.
- `tmux.config_file` applies only when fleet-run starts the server (documented).
- Window-name validation (`:` and `<repo>-<run>` collisions) is enforced in fleet-run only, not
  fleet-task.
- Declining the `rm` / `edit --remove` confirmation exits 1.
- A worktree directory that is already missing does not block `rm`.
- An interrupted `fleet-cache ensure` exits 1 (not 130/143).
- `fleet-task list` reports change status only with `--status`, for speed.
- `fleet-run stop` kills every window with a matching name.
- The cache key leaves out Node version and arch: after switching Node versions, native addons in a
  shared entry can be stale; run `fleet-cache ensure --force <dir>` to rebuild.
- fleet-cache builds an entry in a staging dir holding only `package.json`, `package-lock.json`
  and `.npmrc`: `file:` dependencies, npm workspaces and install scripts that need other repo files
  are not supported (workspaces are rejected up front; the others fail or differ). Run `npm ci` in
  the worktree for such repos.
- No cross-binary version check: the three binaries are built and installed together from one
  module, so skew is unlikely; only the state file carries `schema_version`.
- install.sh keeps the `FLEET_VERSION` override and the relative-`BIN_DIR` fix.
