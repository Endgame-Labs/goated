#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_GO_BIN="${HOME}/.local/goated-go/bin/go"
GO_BIN="${GO_BIN:-}"

if [[ -z "$GO_BIN" && -x "$LOCAL_GO_BIN" ]]; then
  GO_BIN="$LOCAL_GO_BIN"
fi

if [[ -z "$GO_BIN" ]]; then
  GO_BIN="$(command -v go 2>/dev/null || true)"
fi

if [[ -z "$GO_BIN" ]]; then
  echo "Go toolchain not found on PATH." >&2
  echo "Run: scripts/setup_machine.sh doctor" >&2
  echo "Then: scripts/setup_machine.sh install-go" >&2
  exit 1
fi

echo "Building toolbox..."
(
  cd "$ROOT_DIR/tools/toolbox-cli"
  "$GO_BIN" build -o ../toolbox ./cmd/toolbox
)

echo "Building notesmd..."
(
  cd "$ROOT_DIR/tools/notesmd-cli"
  "$GO_BIN" build -o ../notesmd .
)

echo "Built:"
echo "- $ROOT_DIR/tools/toolbox"
echo "- $ROOT_DIR/tools/notesmd"
