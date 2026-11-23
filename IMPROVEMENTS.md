# Improvements backlog

Remaining items after the 2026-11 sweep (see commits f29357c..8bfd896). Build, vet and tests passed in all three modules.

## Untested paths (never run end to end)
- Interactive `fleet-run start` picker flow. The first-window-then-set-option ordering is covered only by unit tests of pure helpers.
- `fleet-cache ensure-windows` / `gc-windows` command paths. Only helpers and generated PowerShell scripts were exercised.
- Windows runtime paths in fleet-task (`windowsGit`, symlinked worktrees).
- Windows staging-to-entry rename is a plain `os.Rename` over `/mnt/c`. Not tested under antivirus/indexer contention.

## Open items
- `--json` only on `fleet-task list`, `fleet-run status`, `fleet-cache gc`/`gc-windows`. Other read commands have none.

## Decisions kept as-is (revisit only if wanted)
- `fleet-task new` is prompt-only (no picker). Nothing to pick.
- `--force` means "rebuild" on `ensure` and "really delete" on `gc`. Help text clarified only.
- `fleet-run/select.go` is a hand-synced copy of fleet-task's picker. No shared code by design.
- `tmux.config_file` applies only when fleet-run starts the server (documented).
- Window-name validation (`:` and `<repo>-<run>` collisions) is enforced in fleet-run only, not fleet-task.
