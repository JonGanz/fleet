# Improvements backlog

No open work items. What remains are paths never exercised end to end and deliberate decisions.

## Untested paths (never run end to end)
- Interactive pickers in `fleet-run start`/`stop` (only their state machines are unit tested; the non-interactive `start`/`stop` flows were run against real tmux).
- `fleet-cache ensure-windows` / `gc-windows` command paths. Only helpers and generated PowerShell scripts were exercised.
- Windows runtime paths in fleet-task (`windowsGit`, symlinked worktrees).
- fleet-run `remain-on-exit`, `logs` and dead-pane respawn on `runtime: windows` targets (`powershell.exe -NoExit` never exits on its own, so a crash there shows as a live pane).
- Windows staging-to-entry rename is a plain `os.Rename` over `/mnt/c`. Not tested under antivirus/indexer contention.

## Decisions kept as-is (revisit only if wanted)
- `--force` means "rebuild" on `ensure` and "really delete" on `gc`. Help text clarified only.
- `fleet-run/select.go` is a hand-synced copy of fleet-task's picker. No shared code by design.
- `tmux.config_file` applies only when fleet-run starts the server (documented).
- Window-name validation (`:` and `<repo>-<run>` collisions) is enforced in fleet-run only, not fleet-task.
