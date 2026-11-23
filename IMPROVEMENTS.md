# Improvements backlog

Open decisions, feature ideas not started, paths never exercised end to end, and deliberate
decisions.

## Needs a decision
- **Repository layout.** Keep three modules (releases then need prefixed tags, `fleet-task/vX.Y.Z`
  ×3) or move to one root module with `cmd/` + `internal/` (one tag; removes the hand-synced
  duplicates: paths, config parsing, ticket validation, winpath, `select.go`,
  `interactiveAllowed`). Recommended: single module, unless a split into separate repos is
  actually planned.
- **LICENSE.** None exists; pick one.
- **Release tooling.** GoReleaser (linux amd64/arm64 + darwin; all three modules cross-compile),
  GitHub Releases, optional Homebrew tap / deb; changelog generated from the conventional commits
  (git-cliff or release-please). Depends on the layout decision.
- **Cache key.** Still only sha256(package-lock.json). It ignores Node version/arch (stale native
  addons after a Node switch) and `.npmrc` (two worktrees with the same lockfile but different
  `.npmrc` share whichever entry was built first). Changing it changes the contract and how `gc`
  matches entries.
- **Hooks.** Let a failing `pre-*` hook veto the operation? Add a timeout (a hanging hook
  currently blocks the command)?
- **Subprocess timeouts** for git / powershell (none today). And `GIT_SSH_COMMAND="ssh -o
  BatchMode=yes"` when headless: would stop SSH passphrase hangs, but overrides
  `core.sshCommand`.
- **Branch cleanup.** `rm` never deletes the local ticket branch from the base (a later `new` of
  the same ticket reattaches it). Add `rm --delete-branch`, or delete when fully pushed?
- **Error-line prefixes** differ across the tools (`error:` vs `fleet-run:` vs
  `fleet-cache <cmd>:`). Unify?
- **Function order.** Reorder functions alphabetically within files (the owner's stated style;
  pure churn, so not done during the fix pass).
- **install.sh extras added unasked:** the `FLEET_VERSION` env var and the relative-`BIN_DIR` fix.
  Keep?
- **CI tool versions.** Pin staticcheck/govulncheck (currently `@latest`).
- **XDG.** Relative `XDG_*_HOME` values are accepted; the spec says to ignore them. Change?
- **Versioning the contract.** A `schema_version` field in the state file / JSON outputs, and a
  contract version checked at the subprocess boundary between the binaries (version skew).

- **Interrupted `new` leaves an unrecorded worktree.** State is written after each finished repo,
  so a `new` killed mid-repo can leave that one repo's worktree on disk with no state entry;
  `edit --add <repo>` then likely fails because the path exists (read from the code, not
  reproduced). Options: record the repo as pending before creating it, or let `edit --add` adopt
  an existing worktree of the right branch.
- **Ticket id source differs between tools.** fleet-task treats the state filename as
  authoritative; fleet-run still uses the file's `ticket` field (`@fleet_task_ticket`,
  `FLEET_TICKET`, JSON). They agree only when the two match. Make fleet-run follow the filename?
- **Lock error advice.** The "task is locked" error tells the user to `rm` the lock file if no
  other fleet-task is running; the agent skill says never to delete it. Reword the error?

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
- `fleet-run/select.go` is a hand-synced copy of fleet-task's picker. No shared code by design.
- `tmux.config_file` applies only when fleet-run starts the server (documented).
- Window-name validation (`:` and `<repo>-<run>` collisions) is enforced in fleet-run only, not
  fleet-task.
- Declining the `rm` / `edit --remove` confirmation exits 1.
- A worktree directory that is already missing does not block `rm`.
- An interrupted `fleet-cache ensure` exits 1 (not 130/143).
- `fleet-task list` reports change status only with `--status`, for speed.
- `fleet-run stop` kills every window with a matching name.
