# Codebase & Architecture

## Project structure

```
.                        # Go module root
├── cmd/
│   ├── goated/          # Agent CLI (./workspace/goat)
│   └── goated/          # CLI + daemon (./goated, ./workspace/goat)
├── internal/
│   ├── app/             # Config (Viper/goated.json + creds)
│   ├── agent/           # Provider-neutral runtime contracts
│   ├── claude/          # Claude headless runtime (claude -p --resume, hooks-based)
│   ├── claudetui/       # Claude TUI runtime implementations (tmux-based)
│   ├── codextui/        # Codex TUI runtime implementations (tmux-based)
│   ├── cron/            # Cron runner
│   ├── db/              # BoltDB persistence (crons, subagent runs, meta)
│   ├── gateway/         # Gateway service (message routing, auto-compact, retry)
│   ├── slack/           # Slack connector (Socket Mode)
│   ├── subagent/        # Headless subagent launcher
│   ├── telegram/        # Telegram connector
│   ├── tmux/            # Shared tmux helpers
│   └── util/            # Markdown conversion, etc.
├── workspace/           # Agent working directory (where the active runtime runs)
│   ├── goat             # Agent CLI binary (built by build.sh)
│   ├── GOATED.md        # Shared runtime instructions
│   ├── CLAUDE.md        # Claude compatibility shim
│   ├── TOOLS.md         # Guide for building CLI tools
│   └── self/            # Private agent data (gitignored)
├── build.sh             # Builds all three binaries
├── build_all_and_run_daemon.sh  # Builds + starts daemon
└── main.go              # Alias entrypoint (same as cmd/goated)
```

## Binaries

| Binary | Source | Output path | Purpose |
|--------|--------|-------------|---------|
| `goated` | `.` (main.go) | `./goated` | Control CLI + daemon (`daemon run`, `start`, `cron`, `bootstrap`) |
| `goat` | `./cmd/goated` | `./workspace/goat` | Agent CLI (used by the runtime inside workspace) |

Both are statically-compiled Go. The daemon uses ~14 MB RSS. The `goat` CLI is exec'd per-call and exits immediately.

## How it works

```
┌──────────┐         ┌──────────────┐  prompt/paste ┌──────────────────────────┐
│  Slack/  │ ──────> │   Gateway    │ ───────────> │  Active Runtime          │
│ Telegram │         │   Daemon     │              │  (headless or tmux)      │
│   User   │ <────── │              │ <──────────  │                          │
└──────────┘         └──────────────┘  exec        └──────────────────────────┘
    ^                    │                           │            │
    │                    │                           │            │ ./goat spawn-subagent
    │                    │         ./goat send_user_ │            │
    │                    │                 message   v            v
    └────────────────────┼────────────────────────────      ┌────────────────────┐
                         │                                  │  Subagent          │
                    ┌────v─────┐                            │ (headless runtime) │
                    │   Cron   │ ────────────────────────>  │                    │
                    │  Runner  │  spawn                     └────────────────────┘
                    └──────────┘
```

**Message flow:**

1. User sends a message via Slack or Telegram
2. Gateway connector receives it (Socket Mode / polling / webhook)
3. Gateway posts a `_thinking..._` indicator (Slack) or typing animation (Telegram)
4. Message is wrapped in a **pydict envelope** (Python dict literal with message, source, chat_id, respond_with, formatting)
5. The selected session runtime delivers the envelope: `claude` runtime spawns `claude -p --resume <sid>` as a subprocess; TUI runtimes paste into the tmux pane
6. Idle detection varies by runtime: `claude` blocks on process exit; TUI runtimes poll with content-change detection (stable pane + `❯` prompt)
7. The active runtime processes the request and pipes markdown into `./goat send_user_message --chat <id>`
8. The `goat` CLI converts markdown to platform format (Slack mrkdwn / Telegram HTML) and posts it
9. On Slack, the thinking indicator is deleted; if the runtime is still busy, a new one is posted and reaped on idle

**Key design choice:** the runtime sends its own replies. The gateway doesn't scrape output from tmux — the runtime is instructed to pipe its response through the `goat` CLI.

**Subagents and cron jobs** run as headless runtime processes (not in the tmux session). All Claude-backed runtimes (`claude` and `claude_tui`) use `claude -p`; `codex_tui` uses `codex exec`. Each gets its own process, tracked in BoltDB with PID and status.

## Gateway features

- **Auto-compact:** checks context usage every 5 messages using the active runtime's context-estimate capability. If usage exceeds 80% and compaction is supported, sends `/compact` and queues incoming messages until done.
- **Retry on API errors:** detects 5xx/overloaded errors in the pane and retries up to 2 times.
- **Session health:** classifies recoverable vs non-recoverable runtime failures. Auto-restarts recoverable failures up to 5 times. DMs admin if recovery fails.
- **Thinking indicator (Slack):** posts `_thinking..._` on message receipt, deletes it when the runtime responds. TTL reaper (4min soft / 20min hard) prevents orphaned indicators.
- **Idle detection:** runtime-specific. Claude uses stable-pane plus prompt detection; Codex uses pane stability plus blocker classification.

## Cron system

- Cron jobs are stored in BoltDB with schedule, prompt, timezone, and flags.
- The runner ticks every minute, checks due jobs, spawns subagents.
- Jobs with `--silent` flag suppress both user messages and main session notifications on success (errors always notify).
- A job won't fire again if its previous run is still in-flight.

## Configuration

Settings live in `goated.json` (Viper-managed). Secrets live in `workspace/creds/*.txt`. Environment variables override both.

### Settings (`goated.json`)

| Key | Default | Description |
|-----|---------|-------------|
| `gateway` | `telegram` | `slack` or `telegram` |
| `agent_runtime` | `claude` | `claude`, `claude_tui`, or `codex_tui` |
| `default_timezone` | `America/Los_Angeles` | Timezone for cron schedules |
| `workspace_dir` | `workspace` | Agent working directory |
| `db_path` | `./goated.db` | BoltDB path |
| `log_dir` | `./logs` | Log directory |
| `telegram.mode` | `polling` | `polling` or `webhook` |
| `telegram.webhook_addr` | `:8080` | Listen address for webhook mode |
| `telegram.webhook_path` | `/telegram/webhook` | Webhook endpoint path |
| `slack.attachments_root` | `workspace/tmp/slack/attachments` | Slack attachment download dir |
| `slack.attachment_max_bytes` | `26214400` | Max single attachment size |
| `slack.attachment_max_total_bytes` | `263192576` | Max total attachment size |
| `slack.attachment_max_parallel` | `3` | Parallel attachment downloads |

### Secrets (`workspace/creds/*.txt`)

| Creds file | Env var override | Description |
|------------|-----------------|-------------|
| `GOAT_TELEGRAM_BOT_TOKEN.txt` | `GOAT_TELEGRAM_BOT_TOKEN` | Telegram bot API token |
| `GOAT_TELEGRAM_WEBHOOK_URL.txt` | `GOAT_TELEGRAM_WEBHOOK_URL` | Public URL for webhook mode |
| `GOAT_SLACK_BOT_TOKEN.txt` | `GOAT_SLACK_BOT_TOKEN` | Bot User OAuth Token (xoxb-...) |
| `GOAT_SLACK_APP_TOKEN.txt` | `GOAT_SLACK_APP_TOKEN` | App-Level Token (xapp-...) |
| `GOAT_SLACK_CHANNEL_ID.txt` | `GOAT_SLACK_CHANNEL_ID` | Monitored Slack DM channel |
| `GOAT_ADMIN_CHAT_ID.txt` | `GOAT_ADMIN_CHAT_ID` | Chat ID for admin alerts |

Env vars always win over creds files. Use `goated creds set KEY VALUE` to manage secrets.
