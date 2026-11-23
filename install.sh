#!/bin/sh
# Install (or with --uninstall, remove) the fleet CLIs and the fleet agent skill.
#
#   binaries   -> $BIN_DIR                         (default ~/.local/bin)
#   skill copy -> $XDG_DATA_HOME/fleet/skills/fleet (default ~/.local/share/...)
#   skill link -> $CLAUDE_CONFIG_DIR/skills/fleet   (default ~/.claude/skills/fleet),
#                 a symlink to the installed copy, so Claude Code sees the
#                 skill that matches the installed binaries -- not whatever
#                 the checkout currently holds.
#
# Binaries are built with -trimpath and stamped with -X main.version=
# $FLEET_VERSION (default: `git describe --tags --always --dirty`, or "dev").
# Each is built to a temp file in $BIN_DIR and moved into place, so a failed
# build never leaves a truncated binary and a running one is replaced safely.
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
  echo "usage: $0 [--uninstall | --help]"
}

# require_go fails early with a clear message instead of a mid-build error.
require_go() {
  if ! command -v go >/dev/null 2>&1; then
    echo "$0: go not found on PATH; install Go (https://go.dev/dl/) first" >&2
    exit 1
  fi
}

# build_version: $FLEET_VERSION if set, else git describe, else "dev".
build_version() {
  if [ -n "${FLEET_VERSION:-}" ]; then
    echo "$FLEET_VERSION"
  else
    git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev
  fi
}

# is_our_link: $SKILL_LINK is a symlink to the installed copy, or to the
# checkout (how earlier install.sh versions linked it).
is_our_link() {
  [ -L "$SKILL_LINK" ] || return 1
  target="$(readlink "$SKILL_LINK")"
  [ "$target" = "$SKILL_DEST" ] || [ "$target" = "$SKILL_SRC" ]
}

# install_binaries builds each binary to a temp file next to its final path
# (same filesystem, so mv is an atomic rename) and only then moves it into
# place. The EXIT trap removes a leftover temp file when a build fails.
install_binaries() {
  require_go
  mkdir -p "$BIN_DIR"
  bin_dir="$(cd "$BIN_DIR" && pwd)" # absolute: the build runs in a subshell cd
  version="$(build_version)"
  tmp=""
  trap 'rm -f "$tmp"' EXIT
  trap 'exit 1' HUP INT TERM
  for name in $BINARIES; do
    echo "building $name ($version) -> $bin_dir/$name"
    tmp="$bin_dir/.$name.tmp.$$"
    (cd "$ROOT/$name" && go build -trimpath -ldflags "-X main.version=$version" -o "$tmp" .)
    mv -f "$tmp" "$bin_dir/$name"
  done
  tmp=""
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
    usage >&2
    exit 2
    ;;
esac
