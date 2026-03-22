#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

GO_VERSION="$(awk '$1 == "go" { print $2; exit }' go.mod)"
CONFIGURED_RUNTIME="${GOAT_AGENT_RUNTIME:-}"
LOCAL_GO_ROOT="${HOME}/.local/goated-go"
LOCAL_GO_BIN="${LOCAL_GO_ROOT}/bin/go"
LOCAL_GOFMT_BIN="${LOCAL_GO_ROOT}/bin/gofmt"

if [[ -z "$CONFIGURED_RUNTIME" && -f .env ]]; then
  CONFIGURED_RUNTIME="$(awk -F= '$1 == "GOAT_AGENT_RUNTIME" { gsub(/["'"'"'"'"'"'"'"'"']/, "", $2); print $2; exit }' .env)"
fi

usage() {
  cat <<EOF
Usage:
  scripts/setup_machine.sh doctor
  scripts/setup_machine.sh install-system
  scripts/setup_machine.sh install-go

Commands:
  doctor          Check required tools for building and running Goated.
  install-system  Install core Ubuntu/Debian packages used by this repo.
  install-go      Install Go ${GO_VERSION} from the official tarball to ${LOCAL_GO_ROOT}.

Notes:
  - Runtime CLIs are validated but not auto-installed here.
  - Claude Code and Codex should be installed and authenticated separately.
EOF
}

have() {
  command -v "$1" >/dev/null 2>&1
}

find_tool() {
  local name="$1"
  case "$name" in
    go)
      if have go; then
        command -v go
        return 0
      fi
      if [[ -x "$LOCAL_GO_BIN" ]]; then
        echo "$LOCAL_GO_BIN"
        return 0
      fi
      ;;
    gofmt)
      if have gofmt; then
        command -v gofmt
        return 0
      fi
      if [[ -x "$LOCAL_GOFMT_BIN" ]]; then
        echo "$LOCAL_GOFMT_BIN"
        return 0
      fi
      ;;
    *)
      if have "$name"; then
        command -v "$name"
        return 0
      fi
      ;;
  esac
  return 1
}

version_ge() {
  local have_version="$1"
  local want_version="$2"
  [[ "$(printf '%s\n%s\n' "$want_version" "$have_version" | sort -V | head -n1)" == "$want_version" ]]
}

go_installed_version() {
  if ! have go; then
    if [[ ! -x "$LOCAL_GO_BIN" ]]; then
      return 1
    fi
  fi
  "$(find_tool go)" version | awk '{print $3}' | sed 's/^go//'
}

check_go_version() {
  local installed
  installed="$(go_installed_version)" || return 1
  version_ge "$installed" "$GO_VERSION"
}

print_status() {
  local name="$1"
  local status="$2"
  printf '%-14s %s\n' "$name" "$status"
}

print_tool() {
  local name="$1"
  local required="$2"
  if have "$name"; then
    print_status "$name" "$(command -v "$name")"
    return 0
  fi
  if path="$(find_tool "$name" 2>/dev/null)"; then
    print_status "$name" "$path"
    return 0
  fi
  if [[ "$required" == "required" ]]; then
    print_status "$name" "missing"
    return 1
  fi
  print_status "$name" "missing (optional)"
  return 0
}

doctor() {
  local failures=0

  echo "Goated machine doctor"
  echo "Repo root: $ROOT_DIR"
  echo "Required Go version: $GO_VERSION"
  if [[ -n "$CONFIGURED_RUNTIME" ]]; then
    echo "Configured runtime: $CONFIGURED_RUNTIME"
  else
    echo "Configured runtime: (not set)"
  fi
  echo
  echo "Core build tools"

  print_tool bash required || failures=1
  print_tool git required || failures=1
  print_tool tmux required || failures=1

  if find_tool go >/dev/null 2>&1; then
    local go_version
    go_version="$(go_installed_version)"
    if check_go_version; then
      print_status go "ok ($go_version)"
    else
      print_status go "too old ($go_version; need >= $GO_VERSION)"
      failures=1
    fi
  else
    print_status go "missing"
    failures=1
  fi

  if path="$(find_tool gofmt 2>/dev/null)"; then
    print_status gofmt "$path"
  else
    print_status gofmt "missing"
    failures=1
  fi

  echo
  echo "Runtime CLIs"
  print_tool claude optional || true
  print_tool codex optional || true

  if [[ "$CONFIGURED_RUNTIME" == "claude" ]] && ! have claude; then
    echo
    echo "Configured runtime requires Claude Code, but 'claude' is not on PATH."
    failures=1
  fi
  if [[ "$CONFIGURED_RUNTIME" == "codex" ]] && ! have codex; then
    echo
    echo "Configured runtime requires Codex, but 'codex' is not on PATH."
    failures=1
  fi

  echo
  echo "Daemon watchdog"
  if crontab -l 2>/dev/null | grep -q 'watchdog\.sh'; then
    print_status "watchdog" "installed (crontab)"
  else
    print_status "watchdog" "not installed"
  fi

  echo
  echo "Helpful next steps"
  if ! find_tool go >/dev/null 2>&1 || ! check_go_version || ! find_tool gofmt >/dev/null 2>&1; then
    echo "- Install Go $GO_VERSION: scripts/setup_machine.sh install-go"
  fi
  if ! crontab -l 2>/dev/null | grep -q 'watchdog\.sh'; then
    echo "- Install watchdog cron: (crontab -l 2>/dev/null; echo '*/2 * * * * $ROOT_DIR/scripts/watchdog.sh') | crontab -"
  fi
  if ! have claude; then
    echo "- Install and authenticate Claude Code if you want GOAT_AGENT_RUNTIME=claude"
  fi
  if ! have codex; then
    echo "- Install and authenticate Codex if you want GOAT_AGENT_RUNTIME=codex"
  fi

  if [[ "$failures" -ne 0 ]]; then
    exit 1
  fi
}

install_system() {
  if [[ ! -r /etc/os-release ]]; then
    echo "Unsupported system: /etc/os-release not found" >&2
    exit 1
  fi

  # shellcheck disable=SC1091
  source /etc/os-release
  if [[ "${ID:-}" != "ubuntu" && "${ID_LIKE:-}" != *"debian"* ]]; then
    echo "install-system currently supports Ubuntu/Debian only." >&2
    exit 1
  fi

  sudo apt-get update
  sudo apt-get install -y \
    bash \
    build-essential \
    ca-certificates \
    cron \
    curl \
    git \
    tar \
    tmux \
    xz-utils
}

install_go() {
  local os arch url archive

  if [[ -x "$LOCAL_GO_BIN" ]]; then
    local installed_version
    installed_version="$("$LOCAL_GO_BIN" version | awk '{print $3}' | sed 's/^go//')"
    if [[ "$installed_version" == "$GO_VERSION" ]]; then
      echo "Go $GO_VERSION already installed at $LOCAL_GO_ROOT"
      return 0
    fi
  fi

  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"

  case "$os" in
    linux) ;;
    *)
      echo "install-go currently supports Linux only." >&2
      exit 1
      ;;
  esac

  case "$arch" in
    x86_64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *)
      echo "Unsupported architecture: $arch" >&2
      exit 1
      ;;
  esac

  archive="/tmp/go${GO_VERSION}.${os}-${arch}.tar.gz"
  url="https://go.dev/dl/go${GO_VERSION}.${os}-${arch}.tar.gz"

  echo "Downloading $url"
  curl -fsSL "$url" -o "$archive"

  echo "Installing Go $GO_VERSION to $LOCAL_GO_ROOT"
  rm -rf /tmp/go
  rm -rf "$LOCAL_GO_ROOT"
  mkdir -p "$(dirname "$LOCAL_GO_ROOT")"
  tar -C /tmp -xzf "$archive"
  rm -rf "$LOCAL_GO_ROOT"
  mv /tmp/go "$LOCAL_GO_ROOT"

  cat <<EOF

Go $GO_VERSION installed under $LOCAL_GO_ROOT.
If $LOCAL_GO_ROOT/bin is not already on your PATH, add this to ~/.profile or ~/.bashrc:

  export PATH="$LOCAL_GO_ROOT/bin:\$PATH"

Then open a new shell and rerun:

  scripts/setup_machine.sh doctor
  ./build.sh
EOF
}

main() {
  local cmd="${1:-doctor}"
  case "$cmd" in
    doctor)
      doctor
      ;;
    install-system)
      install_system
      ;;
    install-go)
      install_go
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      echo "Unknown command: $cmd" >&2
      echo >&2
      usage >&2
      exit 1
      ;;
  esac
}

main "$@"
