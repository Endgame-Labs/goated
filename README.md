# goated

A self-healing bridge between Telegram and Claude Code. Send messages to your agent via Telegram, get responses back. Includes cron jobs, subagents, credential management, and daemon lifecycle.

## Architecture

### Dependencies

- **Go 1.21+** — all binaries are compiled from this repo
- **tmux** — hosts the persistent Claude Code interactive session
- **Claude Code CLI** (`claude`) — must be installed and authenticated (`claude` on PATH)
- **Telegram Bot API** — user-facing interface (bot token from [@BotFather](https://t.me/BotFather))
- **bbolt** — embedded key-value database (no external DB server)

### How it works

```
┌──────────┐         ┌──────────────┐     paste    ┌──────────────────────────┐
│ Telegram │ ──────> │   Gateway    │ ───────────> │  Claude Code (tmux)      │
│   User   │         │  (polling/   │              │  interactive session     │
│          │ <────── │   webhook)   │ <──────────  │                          │
└──────────┘         └──────────────┘  exec        └──────────────────────────┘
    ^                    │                           │            │          
    │                    │                           │            │ ./goat spawn-subagent
    │                    │         ./goat send_user_ │            │
    │                    │                 message   v            v
    └────────────────────┼────────────────────────────      ┌────────────────────┐
                         │                                  │  Subagent          │
                    ┌────v─────┐                            │  (headless claude) │
                    │   Cron   │ ────────────────────────>  │                    │
                    │  Runner  │  spawn                     └────────────────────┘ 
                    └──────────┘         
```

Both the **cron runner** and **Claude Code itself** can spawn subagents. The cron runner does it on a schedule; Claude Code does it via `./goat spawn-subagent` when it wants to delegate a task to a parallel worker. All subagents are tracked in bbolt.

**Steady-state message flow:**

1. User sends a Telegram message
2. Gateway connector receives it via long-polling (or webhook)
3. Gateway service checks Claude session health (auto-restarts if unhealthy)
4. `TmuxBridge.SendAndWait()` pastes the prompt into the tmux pane running Claude Code
5. Bridge polls the pane every 2s waiting for the `❯` prompt to reappear
6. Claude Code processes the request and calls `./goat send_user_message --chat <id>`
7. The `goat` binary reads markdown from Claude's stdout, converts it to Telegram HTML, and sends it back to the user via the Bot API

**Key design choice:** Claude Code sends its own replies. The gateway doesn't scrape output from tmux — instead, Claude is instructed (via `CLAUDE.md`) to pipe its response through the `goat` CLI. This makes the system stateless on the response path and avoids fragile scrollback parsing.

**Subagents and cron jobs** run as headless `claude -p` processes (not in the tmux session). Each gets its own process, tracked in bbolt with PID and status. The cron runner skips a job if its previous run is still in-flight, preventing pile-ups from long-running tasks.

### Folder structure

```
goated/
├── main.go                     # Entry point (builds ./goated)
├── build.sh                    # Builds all three binaries
├── .env                        # Config (gitignored)
├── goated.db                   # bbolt database (gitignored)
│
├── cmd/
│   ├── daemon/main.go          # Daemon binary (builds ./goated_daemon)
│   └── goated/                 # Shared CLI (builds both ./goated and ./goat)
│       └── cli/
│           ├── bootstrap.go    # Interactive setup wizard
│           ├── creds.go        # Credential management
│           ├── cron.go         # Cron CRUD
│           ├── daemon.go       # daemon start/stop/restart/status
│           ├── gateway.go      # Run gateway standalone
│           ├── send_user_message.go  # Agent → Telegram message push
│           ├── spawn_subagent.go     # Launch headless claude
│           └── start.go        # Foreground start (gateway + cron)
│
├── internal/
│   ├── app/config.go           # .env loader and config struct
│   ├── claude/tmux_bridge.go   # tmux session management, paste, health checks
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
│   ├── CLAUDE.md               # Agent instructions (committed, depersonalized)
│   ├── GOATED_CLI_README.md    # Agent CLI reference (committed)
│   ├── CRON.md                 # Instructions for cron-spawned agents (committed)
│   ├── creds/                  # File-backed credentials (gitignored)
│   └── self/                   # Agent's private repo (gitignored, see below)
│
├── docs/
│   └── OPENCLAW_MIGRATION.md   # Migration guide from OpenClaw
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
- `CLAUDE.md` — agent instructions, response contract, daemon rules
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

- Go 1.21+
- tmux
- A Telegram bot token (from [@BotFather](https://t.me/BotFather))
- Claude Code CLI (`claude`) installed and authenticated

### Install

```sh
git clone https://github.com/dorkitude/goated.git
cd goated
bash build.sh
```

This builds three binaries: `./goated`, `./goated_daemon`, and `./goat`.

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

| Variable | Default | Description |
|----------|---------|-------------|
| `GOAT_TELEGRAM_BOT_TOKEN` | (required) | Telegram bot API token |
| `GOAT_DEFAULT_TIMEZONE` | `America/Los_Angeles` | Timezone for cron schedules |
| `GOAT_TELEGRAM_MODE` | `polling` | `polling` or `webhook` |
| `GOAT_ADMIN_CHAT_ID` | (optional) | Chat ID for admin alerts when auto-recovery fails |
| `GOAT_WORKSPACE_DIR` | current directory | Agent working directory |
| `GOAT_DB_PATH` | `./goated.db` | Path to bbolt database |
| `GOAT_LOG_DIR` | `./logs` | Log directory |
| `GOAT_CONTEXT_WINDOW_TOKENS` | `200000` | Context window size estimate |
| `GOAT_TELEGRAM_WEBHOOK_URL` | | Public URL for webhook mode |
| `GOAT_TELEGRAM_WEBHOOK_LISTEN_ADDR` | `:8080` | Listen address for webhook mode |
| `GOAT_TELEGRAM_WEBHOOK_PATH` | `/telegram/webhook` | Webhook endpoint path |

### Start

```sh
# Foreground (dev)
./goated start

# Background daemon (prod)
./goated_daemon
```

To find your chat ID, message the bot and send `/chatid`.

## Telegram commands

- `/clear` — start a new Claude session
- `/chatid` — show your chat ID
- `/context` — approximate context window usage
- `/schedule <cron_expr> | <prompt>` — store a scheduled job

Claude sends replies directly via `./goat send_user_message --chat <chat_id>`.

## Daemon management

```sh
./goated daemon restart --reason "deployed new build"
./goated daemon stop
./goated daemon status
```

Restarts wait for in-flight messages to flush. Reasons are logged to `logs/restarts.jsonl`.

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

## Self-healing

- Session health checks detect auth failures, API errors, and connectivity issues
- Auto-restarts the Claude session up to 5 times (once per minute)
- If recovery fails, DMs the admin chat ID
- On startup, detects orphaned work from previous daemon and waits or recovers
- Telegram update offset is persisted so restarts don't replay old messages
- Cron jobs are deduped — a job won't fire again if its previous run is still in-flight

## Migrating from OpenClaw

See [docs/OPENCLAW_MIGRATION.md](docs/OPENCLAW_MIGRATION.md) for credential migration, cron migration, and example prompts.
