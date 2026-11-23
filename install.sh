#!/bin/sh
# Build the three fleet CLIs and install them into ~/.local/bin (or $BIN_DIR).
set -eu

BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
ROOT="$(cd "$(dirname "$0")" && pwd)"

mkdir -p "$BIN_DIR"
for dir in fleet-task fleet-cache fleet-run; do
  echo "building $dir -> $BIN_DIR/$dir"
  (cd "$ROOT/$dir" && go build -o "$BIN_DIR/$dir" .)
done

# Agent skill: link skills/fleet into ~/.claude/skills (or $SKILLS_DIR) so
# Claude Code picks it up; skipped if something else already lives there.
SKILLS_DIR="${SKILLS_DIR:-$HOME/.claude/skills}"
mkdir -p "$SKILLS_DIR"
if [ -L "$SKILLS_DIR/fleet" ] || [ ! -e "$SKILLS_DIR/fleet" ]; then
  ln -sfn "$ROOT/skills/fleet" "$SKILLS_DIR/fleet"
  echo "linked skill $SKILLS_DIR/fleet -> $ROOT/skills/fleet"
else
  echo "skipping skill: $SKILLS_DIR/fleet exists and is not a symlink" >&2
fi
