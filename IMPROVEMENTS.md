# Improvements backlog

Feature ideas not started, paths never exercised end to end, and deliberate decisions.

## Feature ideas (not started)
- `fleet-task doctor` / `validate`: dependencies, config, orphans (worktree without state, state
  without worktree).
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
- Signal handling in fleet-task and fleet-run: only fleet-cache handles SIGINT/SIGTERM fully
  today. A Ctrl-C during `new`/`edit`/`rm`/`mv` stops the process mid-step; state is persisted per
  repo, a leftover worktree is adopted by the next `new`/`edit --add`, and the lock is left for
  pid-based takeover or `fleet-task unlock`. Hooks and headless network git run in their own
  process group: SIGINT/SIGTERM/SIGHUP received meanwhile are forwarded to that group and then
  end fleet as before, but a SIGKILL of fleet leaves that group running until it finishes (or
  forever, with no timeout left to kill it). Other children (local git, npm via fleet-cache) stay in
  fleet's own group and get a terminal's Ctrl-C directly, with no cleanup.
- Lint configuration file (`staticcheck.conf` or a golangci config) so `make lint` and CI use the
  same, explicit checks.
- Release tooling, deferred until the tool has other users: GoReleaser (linux amd64/arm64 +
  darwin), GitHub Releases, optional Homebrew tap / deb, and a CHANGELOG generated from the
  conventional commits (git-cliff or release-please).

## Untested paths (never run end to end)
- Everything `runtime: windows`: `windowsGit` and symlinked worktrees (including adoption via
  `git.exe worktree list`, `branch -D` after `rm`, and the fetch timeout killing `powershell.exe` —
  whether that also ends the Windows-side `git.exe` is unknown), `fleet-cache
  ensure-windows` / `gc-windows` (only helpers and generated PowerShell scripts were exercised),
  the approximate changes check, PowerShell quoting, interrupt behaviour of Windows-side npm, and
  the staging-to-entry rename (a plain `os.Rename` over `/mnt/c`) under antivirus/indexer
  contention.
- fleet-run `remain-on-exit`, `logs` and dead-pane respawn on `runtime: windows` targets
  (`powershell.exe -NoExit` never exits on its own, so a crash there shows as a live pane).
- `fleet-run attach` and the interactive pickers (no tty in tests; only the pickers' state
  machines are unit tested).
- The CI workflow and dependabot config: never run on GitHub, only parsed as YAML.
- shellcheck was never run locally (not installed).

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
- A missing and an empty `.npmrc` produce different cache keys (at most one extra entry; keeps the
  encoding simple).
- Hooks and headless network git run in their own process group so a timeout kills their children
  too; such a hook can't read the terminal. With a terminal, network git stays in fleet's group so
  ssh/HTTPS prompts still work, and a timeout kills only git itself.
- `rm`'s branch check doesn't fetch: "on a remote" means reachable from a remote-tracking ref as of
  the last fetch.
- The cache key leaves out Node version and arch: after switching Node versions, native addons in a
  shared entry can be stale; run `fleet-cache ensure --force <dir>` to rebuild.
- fleet-cache builds an entry in a staging dir holding only `package.json`, `package-lock.json`
  and `.npmrc`: `file:` dependencies, npm workspaces and install scripts that need other repo files
  are not supported (workspaces are rejected up front; the others fail or differ). Run `npm ci` in
  the worktree for such repos.
- No cross-binary version check: the three binaries are built and installed together from one
  module, so skew is unlikely; only the state file carries `schema_version`.
- install.sh keeps the `FLEET_VERSION` override and the relative-`BIN_DIR` fix.
- CI pins staticcheck (v0.8.2, bumped by hand); govulncheck stays at `@latest` on purpose, to
  surface newly published vulnerabilities.
