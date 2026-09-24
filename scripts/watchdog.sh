#!/usr/bin/env bash
# watchdog.sh — ensures the goated daemon is running AND responsive.
# Add to crontab:  */2 * * * * /path/to/goated/scripts/watchdog.sh
set -euo pipefail

# Cron uses a minimal PATH on macOS and may not see Homebrew-installed
# runtimes such as codex. Include the standard Homebrew locations while
# preserving any PATH supplied by the caller.
export PATH="/opt/homebrew/bin:/usr/local/bin:${PATH:-/usr/bin:/bin:/usr/sbin:/sbin}"

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PID_FILE="$REPO_DIR/logs/goated_daemon.pid"
GOATED_BIN="$REPO_DIR/goated"
WATCHDOG_LOG="$REPO_DIR/logs/watchdog.log"
LOCK_DIR="$REPO_DIR/logs/.watchdog.lock"

# How long a goat send_user_message/send_user_file helper may live before it's
# considered stuck and reaped. The client now self-times-out (~90s), so anything
# past this is a genuine zombie.
STUCK_HELPER_SECONDS=600

log() {
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*" >> "$WATCHDOG_LOG"
}

# Prevent overlapping runs — a restart can take minutes, and cron fires every 2.
# Reap a stale lock (>10 min old) left behind by a killed run.
if [ -d "$LOCK_DIR" ] && [ -n "$(find "$LOCK_DIR" -prune -mmin +10 2>/dev/null)" ]; then
    rmdir "$LOCK_DIR" 2>/dev/null || true
fi
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
    exit 0  # another watchdog run is in progress
fi
trap 'rmdir "$LOCK_DIR" 2>/dev/null || true' EXIT

# Convert ps etime ([[dd-]hh:]mm:ss) to seconds. macOS ps has no etimes column.
etime_to_seconds() {
    awk -F'[-:]' '{
        if (NF==4) print $1*86400+$2*3600+$3*60+$4;
        else if (NF==3) print $1*3600+$2*60+$3;
        else if (NF==2) print $1*60+$2;
        else print 0
    }'
}

# Kill goat send_user_message/send_user_file processes older than the threshold.
# These hang when the daemon's send path wedges; left alive they hold the runtime
# session open and block all message processing.
reap_stuck_helpers() {
    local procs
    # pgrep cannot return elapsed time portably; macOS ps also lacks etimes.
    # shellcheck disable=SC2009
    procs=$(ps -axo pid=,etime=,command= 2>/dev/null \
        | grep -E 'goat send_user_(message|file)' \
        | grep -v grep || true)
    [ -z "$procs" ] && return 0
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local pid etime secs
        pid=$(echo "$line"  | awk '{print $1}')
        etime=$(echo "$line" | awk '{print $2}')
        secs=$(echo "$etime" | etime_to_seconds)
        if [ -n "$secs" ] && [ "$secs" -gt "$STUCK_HELPER_SECONDS" ]; then
            if kill "$pid" 2>/dev/null; then
                log "reaped stuck goat helper pid=$pid age=${secs}s"
            fi
        fi
    done <<< "$procs"
}

# Check if goated binary exists
if [ ! -x "$GOATED_BIN" ]; then
    log "ERROR: goated binary not found at $GOATED_BIN"
    exit 1
fi

# Check if daemon process is alive
daemon_running=false
if [ -f "$PID_FILE" ]; then
    pid=$(cat "$PID_FILE" 2>/dev/null || true)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        daemon_running=true
    fi
fi

cd "$REPO_DIR"

if ! $daemon_running; then
    log "Daemon not running, starting it"
    output=$("$GOATED_BIN" daemon run 2>&1) || true
    log "Started daemon: $output"
    exit 0
fi

# Daemon process is alive — but is the socket actually responsive? A live-but-
# wedged daemon (e.g. handler goroutines stuck on an unreachable gateway API)
# looks healthy to a plain PID check but can't process messages.
if "$GOATED_BIN" daemon status --probe >/dev/null 2>&1; then
    reap_stuck_helpers
    exit 0
fi

log "Daemon pid alive but socket unresponsive — forcing restart"
output=$("$GOATED_BIN" daemon restart --reason "watchdog: daemon socket unresponsive" 2>&1) || true
log "Restarted unresponsive daemon: $output"
reap_stuck_helpers
exit 0
