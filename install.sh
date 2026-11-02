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
