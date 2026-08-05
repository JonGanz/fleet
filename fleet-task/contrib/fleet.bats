#!/usr/bin/env bats
#
# Tests for fleet.sh's `fj` shell function. Uses a fake `fleet-task` stub on
# PATH so these run without a real config/state setup or an interactive
# terminal session.

setup() {
  export FAKE_BIN="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$FAKE_BIN"
  export PATH="$FAKE_BIN:$PATH"
  source "$BATS_TEST_DIRNAME/fleet.sh"
}

write_fake_fleet_task() {
  # $1: stdout to print, $2: exit code
  cat > "$FAKE_BIN/fleet-task" <<EOF
#!/usr/bin/env bash
echo "$1"
exit $2
EOF
  chmod +x "$FAKE_BIN/fleet-task"
}

@test "fj cds to the path printed by fleet-task jump" {
  local target="$BATS_TEST_TMPDIR/some/worktree/dir"
  mkdir -p "$target"
  write_fake_fleet_task "$target" 0

  run bash -c "source '$BATS_TEST_DIRNAME/fleet.sh'; cd '$BATS_TEST_TMPDIR'; fj; pwd"

  [ "$status" -eq 0 ]
  [ "$(echo "$output" | tail -n1)" = "$(cd "$target" && pwd)" ]
}

@test "fj does not cd when fleet-task jump exits non-zero (e.g. selection cancelled)" {
  write_fake_fleet_task "" 1

  run bash -c "source '$BATS_TEST_DIRNAME/fleet.sh'; cd '$BATS_TEST_TMPDIR'; fj; pwd"

  [ "$status" -eq 0 ]
  [ "$(echo "$output" | tail -n1)" = "$BATS_TEST_TMPDIR" ]
}

@test "fj does not cd when fleet-task jump prints a nonexistent path" {
  write_fake_fleet_task "$BATS_TEST_TMPDIR/does/not/exist" 0

  run bash -c "source '$BATS_TEST_DIRNAME/fleet.sh'; cd '$BATS_TEST_TMPDIR'; fj; pwd"

  [ "$status" -eq 0 ]
  [ "$(echo "$output" | tail -n1)" = "$BATS_TEST_TMPDIR" ]
}
