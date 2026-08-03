#!/usr/bin/env bats
#
# Integration test for the fzf multiselect/select wiring used by fleet-task
# (fzfSelect/fzfSelectOne in fzf.go). Stubs `fzf` on PATH with a script that
# deterministically "selects" candidates instead of requiring a real
# interactive terminal, so this runs headlessly while still exercising the
# real stdin/stdout/stderr plumbing between the Go binary and the fzf
# subprocess (not just Go-side unit logic).

setup() {
  export FAKE_BIN="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$FAKE_BIN"

  # Fake fzf: echoes back the first line of whatever it received on stdin.
  # Mirrors real fzf's contract (candidates in via stdin, selection out via
  # stdout) closely enough to prove the Go side wires stdin/stdout correctly.
  cat > "$FAKE_BIN/fzf" <<'EOF'
#!/usr/bin/env bash
head -n1
EOF
  chmod +x "$FAKE_BIN/fzf"
  export PATH="$FAKE_BIN:$PATH"

  export FLEET_STATE_DIR="$BATS_TEST_TMPDIR/state"
  mkdir -p "$FLEET_STATE_DIR/tasks"

  export WT1="$BATS_TEST_TMPDIR/worktrees/PROJ-1/backend"
  mkdir -p "$WT1"
  cat > "$FLEET_STATE_DIR/tasks/PROJ-1.json" <<EOF
{
  "ticket": "PROJ-1",
  "description": "first ticket",
  "created_at": "2026-08-01T00:00:00Z",
  "repos": [
    {"repo": "backend", "branch": "PROJ-1", "worktree_path": "$WT1"}
  ]
}
EOF

  export BIN_DIR="$BATS_TEST_TMPDIR/gobin"
  mkdir -p "$BIN_DIR"
  ( cd "$BATS_TEST_DIRNAME/.." && go build -o "$BIN_DIR/fleet-task" . )
  export PATH="$BIN_DIR:$PATH"
}

@test "fleet-task jump pipes candidates through fzf and prints only the selected path" {
  run fleet-task jump

  [ "$status" -eq 0 ]
  [ "$output" = "$WT1" ]
}

@test "fleet-task jump propagates a non-zero fzf exit (e.g. cancelled) without printing a path" {
  cat > "$FAKE_BIN/fzf" <<'EOF'
#!/usr/bin/env bash
cat >/dev/null
exit 1
EOF
  chmod +x "$FAKE_BIN/fzf"

  run fleet-task jump

  [ "$status" -ne 0 ]
  # stderr carries the error message; stdout (what `fj` actually captures via
  # $(...)) must not contain a worktree path.
  [[ "$output" != *"$WT1"* ]]
}
