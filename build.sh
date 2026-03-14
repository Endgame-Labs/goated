#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

GO_VERSION="$(awk '$1 == "go" { print $2; exit }' go.mod)"
LOCAL_GO_BIN="${HOME}/.local/goated-go/bin/go"
GO_BIN="$(command -v go 2>/dev/null || true)"

if [[ -z "$GO_BIN" && -x "$LOCAL_GO_BIN" ]]; then
  GO_BIN="$LOCAL_GO_BIN"
fi

if [[ -z "$GO_BIN" ]]; then
  echo "Go toolchain not found on PATH." >&2
  echo "This repo requires Go ${GO_VERSION} and gofmt." >&2
  echo "Run: scripts/setup_machine.sh doctor" >&2
  echo "Then: scripts/setup_machine.sh install-go" >&2
  exit 1
fi

mkdir -p workspace

echo "Building control binary: ./goated"
"$GO_BIN" build -o goated .

echo "Building daemon binary: ./goated_daemon"
"$GO_BIN" build -o goated_daemon ./cmd/daemon

echo "Building agent binary: ./workspace/goat"
"$GO_BIN" build -o workspace/goat ./cmd/goated

chmod +x goated goated_daemon workspace/goat

echo "Build complete."
echo "- $ROOT_DIR/goated"
echo "- $ROOT_DIR/goated_daemon"
echo "- $ROOT_DIR/workspace/goat"
