#!/bin/sh
# Install (or with --uninstall, remove) the fleet CLIs and the fleet agent skill.
#
#   binaries   -> $BIN_DIR                         (default ~/.local/bin)
#   skill copy -> $XDG_DATA_HOME/fleet/skills/fleet (default ~/.local/share/...)
#   skill link -> $CLAUDE_CONFIG_DIR/skills/fleet   (default ~/.claude/skills/fleet),
#                 a symlink to the installed copy, so Claude Code sees the
#                 skill that matches the installed binaries -- not whatever
#                 the checkout currently holds.
set -eu

BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/fleet"
CLAUDE_SKILLS_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/skills"
ROOT="$(cd "$(dirname "$0")" && pwd)"

BINARIES="fleet-task fleet-cache fleet-run"
SKILL_SRC="$ROOT/skills/fleet"
SKILL_DEST="$DATA_DIR/skills/fleet"
SKILL_LINK="$CLAUDE_SKILLS_DIR/fleet"

usage() {
  echo "usage: $0 [--uninstall]" >&2
}

# is_our_link: $SKILL_LINK is a symlink to the installed copy, or to the
# checkout (how earlier install.sh versions linked it).
is_our_link() {
  [ -L "$SKILL_LINK" ] || return 1
  target="$(readlink "$SKILL_LINK")"
  [ "$target" = "$SKILL_DEST" ] || [ "$target" = "$SKILL_SRC" ]
}

install_binaries() {
  mkdir -p "$BIN_DIR"
  for name in $BINARIES; do
    echo "building $name -> $BIN_DIR/$name"
    (cd "$ROOT/$name" && go build -o "$BIN_DIR/$name" .)
  done
}

# install_skill copies the skill into the data dir via a staging dir (so a
# failed copy never leaves a half-written skill), then points Claude Code's
# skills dir at it. Anything at the link path that isn't a symlink is left
# alone.
install_skill() {
  mkdir -p "$DATA_DIR/skills"
  staging="$SKILL_DEST.tmp.$$"
  rm -rf "$staging"
  cp -R "$SKILL_SRC" "$staging"
  rm -rf "$SKILL_DEST"
  mv "$staging" "$SKILL_DEST"
  echo "installed skill -> $SKILL_DEST"

  mkdir -p "$CLAUDE_SKILLS_DIR"
  if [ -L "$SKILL_LINK" ] || [ ! -e "$SKILL_LINK" ]; then
    ln -sfn "$SKILL_DEST" "$SKILL_LINK"
    echo "linked $SKILL_LINK -> $SKILL_DEST"
  else
    echo "skipping link: $SKILL_LINK exists and is not a symlink" >&2
  fi
}

uninstall() {
  for name in $BINARIES; do
    if [ -e "$BIN_DIR/$name" ]; then
      rm -f "$BIN_DIR/$name"
      echo "removed $BIN_DIR/$name"
    fi
  done

  if is_our_link; then
    rm -f "$SKILL_LINK"
    echo "removed $SKILL_LINK"
  elif [ -e "$SKILL_LINK" ] || [ -L "$SKILL_LINK" ]; then
    echo "leaving $SKILL_LINK: not a link to fleet's skill" >&2
  fi

  if [ -d "$SKILL_DEST" ]; then
    rm -rf "$SKILL_DEST"
    echo "removed $SKILL_DEST"
  fi
  # Drop the data dirs only if nothing else lives in them.
  rmdir "$DATA_DIR/skills" "$DATA_DIR" 2>/dev/null || true
}

case "${1:-}" in
  "")
    install_binaries
    install_skill
    ;;
  --uninstall)
    uninstall
    ;;
  -h|--help)
    usage
    ;;
  *)
    usage
    exit 2
    ;;
esac
