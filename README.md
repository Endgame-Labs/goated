<p align="center">
  <img src="assets/goated_logo.png" alt="Goated" />
</p>

# goated

A self-healing bridge between Slack/Telegram and an interactive agent runtime. Goated supports Claude Code and Codex, with the same chat surface, cron jobs, subagents, credential management, and daemon lifecycle.

> **For AI agents working on this codebase:** see [CODEBASE.md](CODEBASE.md) for architecture and [AGENTS.md](AGENTS.md) for build/run instructions.

## Architecture

### Dependencies

- **Go matching `go.mod`** — all binaries are compiled from this repo
- **tmux** — hosts the persistent interactive runtime session
- **One agent runtime CLI** — `claude` for Claude Code or `codex` for Codex
- **Telegram Bot API** — user-facing interface (bot token from [@BotFather](https://t.me/BotFather))
- **bbolt** — embedded key-value database (no external DB server)

### Footprint

| Binary      | Size  | Description                                                         |
| ----------- | ----- | ------------------------------------------------------------------- |
| `goated`    | 11 MB | Control-plane CLI + daemon (`daemon run`, `start`, cron, bootstrap) |
| `goat`      | 11 MB | Agent-facing CLI (send_user_message, creds, cron, spawn-subagent)   |
| `goated.db` | 64 KB | bbolt embedded database (crons, subagent runs, metadata)            |

Both binaries are statically-compiled Go with no runtime dependencies.

**Memory at runtime:** the daemon uses ~14 MB RSS. Subagents are separate runtime CLI processes. The goat CLI is exec'd per-call and exits immediately, so it adds no persistent memory cost.

For a detailed comparison of token usage, file sizes, and memory overhead vs. OpenClaw, see [docs/PERFORMANCE.md](docs/PERFORMANCE.md).

### How it works

```
┌──────────┐         ┌──────────────┐     paste    ┌──────────────────────────┐
│ Telegram │ ──────> │   Gateway    │ ───────────> │  Active Runtime (tmux)   │
│   User   │         │  (polling/   │              │  interactive session     │
│          │ <────── │   webhook)   │ <──────────  │                          │
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

Both the **cron runner** and the **active runtime session** can spawn subagents. The cron runner does it on a schedule; the runtime does it via `./goat spawn-subagent` when it wants to delegate a task to a parallel worker. All subagents are tracked in bbolt.

**Steady-state message flow:**

1. User sends a message via Slack or Telegram
2. Gateway connector receives it (Socket Mode for Slack, long-polling/webhook for Telegram)
3. Gateway posts a feedback indicator — `_thinking..._` on Slack, typing animation on Telegram
4. Gateway checks active runtime session health (auto-restarts if unhealthy)
5. The message is wrapped in a **pydict envelope** — a Python dict literal containing the message, source channel, chat ID, response command, and which formatting doc to use
6. `TmuxBridge.SendAndWait()` pastes the envelope into the tmux pane via `tmux load-buffer` + `paste-buffer` and presses Enter
7. The bridge polls the pane every 2s using **content-change idle detection**: the pane must be stable (unchanged across consecutive captures) AND contain the `❯` prompt to count as idle — a single prompt check isn't enough because `❯` is often visible while Claude is actively working
8. The active runtime processes the request and pipes its markdown response into `./goat send_user_message --chat <id>`
9. The `goat` CLI converts markdown to the appropriate format (Slack mrkdwn or Telegram HTML) and posts it via the platform API
10. On Slack, the thinking indicator is deleted; if the runtime is still busy, a new one is posted and reaped when it goes idle

**Key design choice:** the runtime sends its own replies. The gateway doesn't scrape output from tmux — instead, the runtime is instructed through the workspace contract to pipe its response through the `goat` CLI. This makes the system stateless on the response path and avoids fragile scrollback parsing.

**Subagents and cron jobs** run as headless runtime processes (not in the tmux session). Claude uses `claude -p`; Codex uses `codex exec`. Each gets its own process, tracked in bbolt with PID and status. The cron runner skips a job if its previous run is still in-flight, preventing pile-ups from long-running tasks.

### Folder structure

```
goated/
├── main.go                     # Entry point (builds ./goated)
├── build.sh                    # Builds all three binaries
├── .env                        # Config (gitignored)
├── goated.db                   # bbolt database (gitignored)
│
├── cmd/
│   └── goated/                 # Shared CLI (builds ./goated and ./workspace/goat)
│       └── cli/
│           ├── bootstrap.go    # Interactive setup wizard
│           ├── creds.go        # Credential management
│           ├── cron.go         # Cron CRUD
│           ├── daemon.go       # daemon run/start/stop/restart/status
│           ├── gateway.go      # Run gateway standalone
│           ├── send_user_message.go  # Agent → Telegram message push
│           ├── session.go      # Active runtime session management (status/restart/send)
│           ├── spawn_subagent.go     # Launch headless runtime worker
│           └── start.go        # Foreground start (gateway + cron)
│
├── internal/
│   ├── app/config.go           # .env loader and config struct
│   ├── agent/                  # Provider-neutral runtime contracts
│   ├── claude/                 # Claude runtime implementations
│   ├── codex/                  # Codex runtime implementations
│   ├── cron/runner.go          # Cron scheduler (1min tick, dedup, 1hr timeout)
│   ├── db/db.go                # bbolt store (open-per-op, no held locks)
│   ├── gateway/
│   │   ├── service.go          # Message routing, health checks, error handling
│   │   └── types.go            # Handler/Responder/Connector interfaces
│   ├── subagent/run.go         # Shared subagent execution (sync + background)
│   ├── telegram/connector.go   # Telegram polling/webhook, offset persistence
│   └── util/                   # Markdown→HTML, text sanitization
│
├── workspace/                  # Agent working directory (GOAT_WORKSPACE_DIR)
│   ├── goat                    # Agent CLI binary (gitignored)
│   ├── GOATED.md               # Shared runtime instructions
│   ├── CLAUDE.md               # Claude compatibility shim
│   ├── GOATED_CLI_README.md    # Agent CLI reference (committed)
│   ├── CRON.md                 # Instructions for cron-spawned agents (committed)
│   ├── creds/                  # File-backed credentials (gitignored)
│   └── self/                   # Agent's private repo (gitignored, see below)
│
├── docs/
│   ├── OPENCLAW_MIGRATION.md   # Migration guide from OpenClaw
│   └── PERFORMANCE.md          # Token, memory, and resource comparison vs OpenClaw
│
└── logs/                       # All logs (gitignored)
    ├── goated_daemon.log
    ├── goated_daemon.pid
    ├── restarts.jsonl
    ├── cron/
    │   ├── runs.jsonl
    │   └── jobs/               # Per-run subagent logs
    ├── subagent/jobs/           # spawn-subagent logs
    └── telegram/               # Chat logs
```

### What's committed vs. what's personal

Everything committed in `workspace/` is **depersonalized and reusable** — it's the platform contract that any agent can pick up. Personal state lives in `workspace/self/`, which is gitignored from this repo.

**Committed (platform):**

- `GOATED.md` — shared runtime instructions and response contract
- `CLAUDE.md` — Claude compatibility shim
- `GOATED_CLI_README.md` — CLI reference
- `CRON.md` — instructions for cron-spawned agents

**Gitignored (personal, lives in `workspace/self/`):**

- `IDENTITY.md` — name, personality, voice
- `MEMORY.md` — long-term memory (loaded every session)
- `USER.md` — info about the human they work with
- `SOUL.md` — values and beliefs
- `AGENTS.md` — workspace conventions and safety rules
- `TODO.md` — agent's personal task list
- `HEARTBEAT.md` — heartbeat/pulse config and prompts
- Projects, notes, drafts, tools, and anything else the agent creates

**We recommend making `workspace/self/` its own private Git repo.** This lets the agent:

- Version its own identity, memory, and project files
- Push/pull independently of the goated platform
- Survive workspace resets without losing accumulated context

To set this up:

```sh
cd workspace/self
git init
git remote add origin git@github.com:your-org/agent-self.git
```

Then add to `workspace/self/AGENTS.md` or similar:

```
Your self/ directory is a private git repo. Commit and push meaningful changes
to your identity, memory, and project files regularly.
```

## Setup

### Prerequisites

- Go matching `go.mod` (currently `1.25.0`)
- tmux
- A Telegram bot token (from [@BotFather](https://t.me/BotFather))
- One runtime CLI installed and authenticated:
  - Claude Code (`claude`)
  - Codex (`codex`)

### Machine bootstrap

Validate the machine before building:

```sh
scripts/setup_machine.sh doctor
```

On Ubuntu/Debian, you can install core packages with:

```sh
scripts/setup_machine.sh install-system
scripts/setup_machine.sh install-go
```

### Install

```sh
git clone https://github.com/dorkitude/goated.git
cd goated
bash build.sh
```

This builds two binaries: `./goated` and `./workspace/goat`.

### Configure

Run the interactive bootstrap:

```sh
./goated bootstrap
```

This creates a `.env` file with your settings. You can also create it manually:

```sh
# .env
GOAT_TELEGRAM_BOT_TOKEN=your-bot-token
GOAT_DEFAULT_TIMEZONE=America/Los_Angeles
GOAT_TELEGRAM_MODE=polling
GOAT_ADMIN_CHAT_ID=your-chat-id
```

All env vars:

| Variable                            | Default               | Description                                       |
| ----------------------------------- | --------------------- | ------------------------------------------------- |
| `GOAT_TELEGRAM_BOT_TOKEN`           | (required)            | Telegram bot API token                            |
| `GOAT_AGENT_RUNTIME`                | `claude`              | `claude` or `codex`                               |
| `GOAT_DEFAULT_TIMEZONE`             | `America/Los_Angeles` | Timezone for cron schedules                       |
| `GOAT_TELEGRAM_MODE`                | `polling`             | `polling` or `webhook`                            |
| `GOAT_ADMIN_CHAT_ID`                | (optional)            | Chat ID for admin alerts when auto-recovery fails |
| `GOAT_WORKSPACE_DIR`                | `workspace`           | Agent working directory                           |
| `GOAT_DB_PATH`                      | `./goated.db`         | Path to bbolt database                            |
| `GOAT_LOG_DIR`                      | `./logs`              | Log directory                                     |
| `GOAT_TELEGRAM_WEBHOOK_URL`         |                       | Public URL for webhook mode                       |
| `GOAT_TELEGRAM_WEBHOOK_LISTEN_ADDR` | `:8080`               | Listen address for webhook mode                   |
| `GOAT_TELEGRAM_WEBHOOK_PATH`        | `/telegram/webhook`   | Webhook endpoint path                             |

### Start

```sh
# Foreground (dev)
./goated start

# Background daemon (prod)
./goated daemon run
```

To find your chat ID, message the bot and send `/chatid`.

## Chat commands

- `/clear` — start a fresh active-runtime session
- `/chatid` — show your chat ID
- `/context` — approximate context window usage
- `/schedule <cron_expr> | <prompt>` — store a scheduled job

The active runtime sends replies directly via `./goat send_user_message --chat <chat_id>`.

## Session management

```sh
./goated session status                     # Health, busy state, context estimate
./goated session restart                    # Kill and restart the active runtime tmux session
./goated session send /context              # Send a slash command or text to the active runtime
./goated session send "What are you working on?"
```

`session send` pastes text directly into the active runtime tmux pane and presses Enter. Useful for sending runtime slash commands (`/context`, `/clear`) or ad-hoc prompts without going through the gateway.

## Runtime management

```sh
./goated runtime status
./goated runtime switch codex
./goated runtime cleanup
```

## Daemon management

```sh
./goated daemon restart --reason "deployed new build"
./goated daemon stop
./goated daemon status
```

Restarts wait for in-flight messages to flush. Reasons are logged to `logs/restarts.jsonl`.

### Watchdog cron

A cron watchdog ensures the daemon is always running. If the daemon dies for any reason, it will be restarted within 2 minutes:

```sh
# Install the watchdog:
(crontab -l 2>/dev/null; echo '*/2 * * * * /path/to/goated/scripts/watchdog.sh') | crontab -
```

Logs to `logs/watchdog.log`.

## Agent CLI

The agent's tmux session runs inside `workspace/`, so all agent commands use `./goat` (not `workspace/goat`).

```sh
# Send message to user
echo "Hello" | ./goat send_user_message --chat <chat_id>

# Credentials
./goat creds set API_KEY value
./goat creds get API_KEY
./goat creds list

# Cron jobs
./goat cron add --chat <chat_id> --schedule "0 8 * * *" --prompt "Morning summary"
./goat cron add --chat <chat_id> --schedule "0 8 * * *" --prompt-file /path/to/prompt.md
./goat cron list
./goat cron disable <id>
./goat cron enable <id>
./goat cron remove <id>

# Subagents
./goat spawn-subagent --prompt "Run a background task"
```

## Reliability features

### Idle detection

Claude uses prompt-aware idle detection: the pane must be stable and show `❯`. Codex uses a runtime-specific state classifier that distinguishes ready, generating, auth-blocked, and intervention-blocked screens instead of relying on a shared prompt glyph.

### Thinking indicators (Slack)

On message receipt, the daemon posts `_thinking..._` to Slack. When the runtime sends its reply via `goat send_user_message`, the CLI deletes the thinking message. If the runtime is still busy, a new thinking indicator is posted and tracked. A TTL reaper acts as a safety net: soft deadline at 4 minutes, hard deadline at 20 minutes.

### Auto-compaction

Every 5 messages, the gateway asks the active runtime for a context estimate. If the runtime reports a known usage above 80% and supports compaction, Goated sends `/compact` and queues any incoming messages until compaction finishes, then flushes the queue.

## Self-healing

- Session health checks detect auth failures, API errors, and connectivity issues
- Auto-restarts the active runtime session up to 5 times (once per minute)
- If recovery fails, DMs the admin chat ID
- On startup, detects orphaned work from previous daemon and waits or recovers
- Telegram update offset is persisted so restarts don't replay old messages
- Cron jobs are deduped — a job won't fire again if its previous run is still in-flight
- **Restart guardian:** `goated daemon restart` spawns a detached safety-net process that ensures the new daemon starts even if the restart command itself is interrupted
- **Watchdog cron:** optional `scripts/watchdog.sh` checks every 2 minutes that the daemon is alive and restarts it if not

## Migrating from OpenClaw

See [docs/OPENCLAW_MIGRATION.md](docs/OPENCLAW_MIGRATION.md) for credential migration, cron migration, and example prompts.

## License

MIT License. Copyright (c) 2025-2026 Kyle Wild and Endgame Labs, Inc. See [LICENSE](LICENSE) for details.
