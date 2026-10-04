# Codebase & Architecture

## Project structure

```
.                        # Go module root
├── cmd/goated/cli/      # CLI commands (Cobra) — 26 .go files
├── internal/
│   ├── agent/           # Provider-neutral runtime contracts & envelope encoding
│   ├── app/             # Config loading (goated.json + env vars + creds)
��   ├── claude/          # Claude headless runtime (claude -p --resume)
│   ├── claudetui/       # Claude TUI runtime (tmux bridge)
│   ��── codex/           # Codex headless runtime (codex exec)
│   ├── codextui/        # Codex TUI runtime (tmux bridge)
│   ├── cron/            # Cron runner (per-minute tick, timezone-aware)
│   ├─�� db/              # BoltDB persistence (crons, subagent runs, channels, meta)
│   ├── gateway/         # Gateway service (message routing, auto-compact, queueing)
│   ├── goatlog/         # Daily-rotating log writer
│   ├── msglog/          # Message logging, redaction, session tracking, replay
│   ├── pydict/          # Python dict literal encoding/parsing
│   ├── runtime/         # Runtime factory (selects provider from config)
│   ├── sessionname/     # Consistent tmux session naming
│   ├── slack/           # Slack connector (Socket Mode + Web API)
│   ���── subagent/        # Headless subagent launcher & completion handler
│   ├── telegram/        # Telegram connector (polling + webhook)
│   ├── tmux/            # Low-level tmux operations (paste, capture, detect)
│   └── util/            # Markdown conversion, sanitization, text helpers
├── workspace/           # Agent working directory
│   ├── GOATED.md        # Shared runtime contract
│   ├── CLAUDE.md        # Claude compatibility shim → AGENTS.md
│   ├── TOOLS.md         # Guide for building custom CLI tools
│   ├── PYDICT_FORMAT.md # Envelope format spec
│   ├── _self.example/   # Bootstrap template for new agents
│   └── self/            # Private agent data (gitignored, separate repo)
├── scripts/
│   ├── setup_machine.sh # Install Go, tmux, runtime CLI; validate environment
│   └── watchdog.sh      # Cron watchdog (every 2 min, auto-restart daemon)
├── docs/
│   ���── PERFORMANCE.md   # Goated vs OpenClaw comparison
│   └── OPENCLAW_MIGRATION.md # Migration guide
├── build.sh             # Builds both binaries
├── main.go              # Entry point (delegates to cmd/goated/cli)
└── goated.json.example  # Configuration template
```

## Binaries

| Binary | Output path | Purpose |
|--------|-------------|---------|
| `goated` | `./goated` | Control-plane CLI + daemon (`daemon run`, `start`, `cron`, `bootstrap`) |
| `goat` | `./workspace/goat` | Agent-facing CLI (send_user_message, creds, cron, spawn-subagent) |

Both are the same Go binary with different names. Statically compiled, no runtime dependencies. The daemon uses ~15-20 MB RSS. The `goat` CLI is exec'd per-call and exits immediately.

## How it works

```
┌──────────┐         ┌──��───────────┐  prompt/paste ┌──���───────────────────────┐
│  Slack/  │ ──────> │   Gateway    │ ───────────> │  Active Runtime          ���
│ Telegram │         │   Daemon     │              │  (headless or tmux)      │
│   User   │ <─────�� │              │ <───��──────  │                          │
└────────��─┘         └────────────���─┘  exec        └──────────────────────────┘
    ^                    │                           │            │
    │                    │                           │            │ ./goat spawn-subagent
    │                    │         ./goat send_user_ ���            │
    │                    │                 message   v            v
    └──���─────────────────┼───────���────────────────────      ┌──────���─────────────┐
                         │                                  │  Subagent          │
                    ��────v─────┐                            │ (headless runtime) │
                    │   Cron   │ ──────��─────────────────>  │                    │
                    ��  Runner  │  spawn                     └────────────────────┘
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

**Headless runtimes** use process-per-message execution. **TUI runtimes** (`claude_tui`, `codex_tui`) run inside tmux. Subagents and cron jobs always run headlessly. Each run is tracked in BoltDB with PID and status.

---

## Module reference

### `cmd/goated/cli/` — CLI commands

All commands use Cobra and are registered into `rootCmd` in `root.go`.

| File | Commands | Purpose |
|------|----------|---------|
| `root.go` | `Execute()` | Entry point; initializes daily-rotating logs |
| `daemon.go` | `daemon run`, `daemon restart`, `daemon stop`, `daemon status` | Daemon lifecycle; PID management; graceful shutdown with 2m message flush; stuck message replay |
| `start.go` | `start` | Foreground gateway (no PID file, for testing) |
| `session.go` | `session restart`, `session status`, `session send` | Runtime session management |
| `send_user_message.go` | `send_user_message` | Queues messages via Unix socket to daemon; reads markdown from stdin |
| `send_user_file.go` | `send_user_file` | Sends files/screenshots to user via daemon |
| `spawn_subagent.go` | `spawn-subagent` | Launches headless subagent with `--prompt`, `--chat` |
| `subagent_finish.go` | `subagent-finish` (hidden) | Records subagent completion; notifies main session |
| `cron.go` | `cron run`, `cron add`, `cron list`, `cron remove`, `cron enable/disable`, `cron set-schedule/set-timezone/set-notify-*` | Full cron management |
| `logs.go` | `logs`, `logs raw`, `logs restarts`, `logs cron`, `logs watchdog` | Log viewing with filtering |
| `logs_turns.go` | `logs turns` | Session-specific message log viewing |
| `doctor.go` | `doctor` | Diagnostics: validates config, runtime binary, tmux, workspace, database, gateway |
| `bootstrap.go` | `bootstrap` | Interactive setup: init DB, workspace, first channel; seeds default crons |
| `log_hook.go` | `log-hook` (hidden) | Logs Claude Code hook events to daily JSONL; redacts credentials |
| `creds.go` | `creds set`, `creds get`, `creds list` | File-backed credential management |
| `channel.go` | `channel list`, `channel add`, `channel delete`, `channel select` | Multi-channel management |
| `slack.go` | `slack history` | Slack message inspection with pagination |
| `helpers.go` | `prompt()`, `promptSecret()` | Interactive CLI I/O utilities |

### `internal/agent/` — Runtime contracts & envelope encoding

**`types.go`** — Provider-neutral abstractions:

- `RuntimeProvider` — string enum: `"claude"`, `"codex"`, `"claude_tui"`, `"codex_tui"`
- `SessionRuntime` interface — `EnsureSession`, `StopSession`, `SendUserPrompt`, `SendBatchPrompt`, `GetHealth`, `GetContextEstimate`, `WaitForAwaitingInput`, `DetectRetryableError`
- `HeadlessRuntime` interface — `RunSync`, `RunBackground`
- `Runtime` interface — combines `Session()` and `Headless()`

**`envelope.go`** — Encodes messages as Python dict literals for delivery to the runtime:

- `BuildPromptEnvelope()` — single message with attachments
- `BuildBatchEnvelope()` — multi-message batch
- `BuildSystemNoticeEnvelope()` — internal notices (cron results, subagent completions)

**`sent_message_log.go`** — Tracks messages sent by subagents via `GOATED_SENT_MESSAGE` log markers.

### `internal/gateway/` — Message routing & service

**`service.go`** — Core message router:

- `Service` struct — holds session runtime, store, message logger, drain context
- `HandleMessage()` — routes user messages to the active runtime
- `HandleBatchMessage()` — multi-message delivery
- `WaitInflight()` — blocks until all in-flight messages complete (graceful shutdown)
- Handles slash commands: `/clear`, `/chatid`, `/context`, `/schedule`
- Auto-compact: checks context usage every 5 messages, compacts at >80%
- Queues messages during compaction

**`types.go`** — `IncomingMessage`, `Responder`, `MediaResponder` interfaces.

### `internal/claude/` — Claude Code headless runtime

**`session.go`** — `SessionRuntime` for `claude` provider (non-tmux, process-per-message).

**`headless.go`** — Headless execution via `claude -p --resume <session_id>`.

**`hooks.go`** — Writes Claude Code hooks config to `workspace/.claude/settings.local.json`. Logged events: SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Stop, PreCompact, TaskCompleted, and more. All events piped to `./goat log-hook` for timestamping and credential redaction.

### `internal/claudetui/` — Claude Code TUI runtime (tmux)

**`tmux_bridge.go`** — The main interactive session runtime:

- `TmuxBridge` struct — manages the persistent Claude Code process inside tmux
- `SendUserPrompt()` — pastes pydict envelope into tmux pane
- `EnsureSession()` — starts tmux session if not running
- `IsSessionBusy()` — content-change detection (2s apart)
- `WaitForAwaitingInput()` — polls for idle state (stable pane + `❯` prompt)
- `GetContextEstimate()` — parses `/context` output
- `GetHealth()` — classifies session state (healthy, recoverable, non-recoverable)

**`headless.go`** ��� Headless execution for TUI-based subagents.

### `internal/codex/` and `internal/codextui/` — Codex runtimes

Mirror the claude/claudetui structure for OpenAI Codex. `codex exec` for headless, tmux bridge for TUI.

### `internal/cron/` — Cron runner

**`runner.go`**:

- `Runner` struct — holds store, workspace dir, headless runtime, notifier
- `Run()` — called every minute; parses 5-field cron schedules in each job's timezone
- Skips jobs whose previous run is still in-flight
- Two job types: `subagent` (spawns headless agent with prompt) and `system` (runs shell command)
- Logs all runs to `cron/runs.jsonl`

### `internal/db/` — BoltDB persistence

**`db.go`** — Open-per-operation pattern (no held connections):

**Buckets:**

| Bucket | Contents |
|--------|----------|
| `crons` | CronJob records (schedule, prompt/command, timezone, notification flags) |
| `cron_runs` | CronRun execution logs (status, log path, runtime info) |
| `subagent_runs` | SubagentRun tracking (PID, status, prompt, timestamps) |
| `meta` | Key-value metadata |
| `channels` | Channel configuration (gateway, chat ID, active flag) |

**Key operations:** `ActiveCrons()`, `SaveCron()`, `DeleteCron()`, `RunningSubagents()`, `RecordSubagentFinish()`, `AllChannels()`, `GetMeta()`, `SetMeta()`

### `internal/slack/` — Slack connector

**`connector.go`**:

- Socket Mode for receiving events; Web API for sending
- Event deduplication via `seenEvents` map (Slack retries)
- Attachment support: downloads files to `workspace/tmp/slack/attachments/`
  - Allowed: PDF, CSV, XLSX, DOCX, PNG, JPG (25 MB max per file, 251 MB total)
  - Auto-sweep old files every 4 hours (30-day retention)
- `SendMessage()` — delivers markdown converted to Slack mrkdwn
- `SendMedia()` — uploads files with optional caption

**`thinking.go`** — Posts/deletes `_thinking..._` indicator messages with TTL reaper (4min soft / 20min hard).

### `internal/telegram/` — Telegram connector

**`connector.go`**:

- Supports both polling and webhook modes
- `SendMessage()` — delivers markdown converted to Telegram HTML
- `SendMedia()` — uploads files/photos with caption
- Attachment handling similar to Slack connector

### `internal/subagent/` — Subagent lifecycle

**`run.go`**:

- `BuildPreamble()` — loads GOATED.md, self/CLAUDE.md, self/AGENTS.md
- `BuildPrompt()` — combines preamble + user prompt + cron context
- `RunSync()` — synchronous headless execution
- `RunBackground()` — detached process with guardian wrapper
- `HandleCompletion()` — records status to DB, notifies main session via tmux paste
- `NotifyMainSession()` — builds system notice envelope and pastes into main tmux session
- Extracts `GOATED_SENT_MESSAGE` markers from subagent logs for notification context

### `internal/msglog/` — Message logging & redaction

**`logger.go`** — Structured JSONL logging:

- `logs/message_logs/daily/YYYY-MM-DD.jsonl` — all daily messages
- `logs/message_logs/sessions/SESSION_ID.jsonl` — per-session conversation
- Request ID correlation across user message → agent response

**`redact.go`** — Auto-redacts credential values from `workspace/creds/*.txt` in all logged output.

**`replay.go`** — Detects stuck messages (sent_to_agent but never responded) and replays them on daemon restart.

### `internal/tmux/` — Low-level tmux operations

**`tmux.go`**:

- `PasteAndEnter()` / `PasteAndEnterFor()` — loads text to tmux buffer, pastes, polls for content change, sends Enter
- `CapturePane()` / `CaptureVisible()` — full scrollback vs visible portion
- `SessionExists()` / `SessionExistsFor()` — check for named sessions
- Content-change detection for idle/busy state

### `internal/pydict/` — Python dict encoding

**`encode.go`** / **`parse.go`**:

- `Encode()` — map to Python dict literal (sorted keys)
- `EncodeOrdered()` — preserves key order via `[]KV`
- Handles: multiline strings (triple-quoted), nil→None, bool→True/False, nested maps/lists
- This is the wire format between goated and the agent runtime

### `internal/runtime/` — Runtime factory

**`factory.go`**:

- `New(cfg)` — returns the correct `agent.Runtime` based on `cfg.AgentRuntime`
- Four providers: `claude`, `claude_tui`, `codex`, `codex_tui`

### `internal/app/` — Configuration

**`config.go`**:

- `LoadConfig()` — reads `goated.json` via Viper, overlays env vars and creds files
- `Config` struct covers: gateway, runtime, model, paths, Slack/Telegram settings, timezone, admin chat ID

---

## Gateway features

- **Auto-compact:** checks context usage every 5 messages. Compacts at >80% usage. Queues incoming messages during compaction.
- **Retry on API errors:** detects 5xx/overloaded errors and retries up to 2 times.
- **Session health:** classifies recoverable vs non-recoverable runtime failures. Auto-restarts up to 5 times. DMs admin if recovery fails.
- **Thinking indicator (Slack):** posts `_thinking..._` on receipt, deletes on response. TTL reaper prevents orphaned indicators.
- **Graceful shutdown:** `daemon restart` flushes in-flight messages (2m timeout) before stopping.
- **Stuck message replay:** on daemon restart, detects messages that were sent_to_agent but never responded and replays them.

## Cron system

- Jobs stored in BoltDB with schedule, prompt/command, timezone, and notification flags.
- Runner ticks every minute, checks due jobs against their timezone.
- Two types: `subagent` (spawns headless agent) and `system` (runs shell command).
- Won't fire again if previous run is still in-flight.
- `notify_user` — sends result to user's chat. `notify_main_session` — pastes notice into main tmux session.
- Bootstrap seeds two default crons: hourly heartbeat (goal review and mission progress) and dreaming (memory consolidation every 8h); see [goals and dreaming](docs/GOALS_AND_DREAMS.md).

## Configuration

Settings live in `goated.json` (Viper-managed). Secrets live in `workspace/creds/*.txt`. Environment variables override both.

### Settings (`goated.json`)

| Key | Default | Description |
|-----|---------|-------------|
| `gateway` | `telegram` | `slack` or `telegram` |
| `agent_runtime` | `claude` | `claude`, `codex`, `claude_tui`, or `codex_tui` |
| `model` | `""` | Claude model (`sonnet`, `opus`, etc.) |
| `default_timezone` | `America/Los_Angeles` | Timezone for cron schedules |
| `workspace_dir` | `workspace` | Agent working directory |
| `db_path` | `./goated.db` | BoltDB path |
| `log_dir` | `./logs` | Log directory |
| `debug` | `false` | Enable debug logging |
| `slack.channel_id` | `""` | Monitored Slack DM/channel ID |
| `slack.attachments_root` | `workspace/tmp/slack/attachments` | Slack attachment download dir |
| `slack.attachment_max_bytes` | `26214400` | Max single attachment size (25 MB) |
| `slack.attachment_max_total_bytes` | `263192576` | Max total attachment size (251 MB) |
| `slack.attachment_max_parallel` | `3` | Parallel attachment downloads |
| `telegram.mode` | `polling` | `polling` or `webhook` |
| `telegram.webhook_addr` | `:8080` | Listen address for webhook mode |
| `telegram.webhook_path` | `/telegram/webhook` | Webhook endpoint path |

### Secrets (`workspace/creds/*.txt`)

| Creds file | Env var override | Description |
|------------|-----------------|-------------|
| `GOAT_TELEGRAM_BOT_TOKEN.txt` | `GOAT_TELEGRAM_BOT_TOKEN` | Telegram bot API token |
| `GOAT_TELEGRAM_WEBHOOK_URL.txt` | `GOAT_TELEGRAM_WEBHOOK_URL` | Public URL for webhook mode |
| `GOAT_SLACK_BOT_TOKEN.txt` | `GOAT_SLACK_BOT_TOKEN` | Bot User OAuth Token (xoxb-...) |
| `GOAT_SLACK_APP_TOKEN.txt` | `GOAT_SLACK_APP_TOKEN` | App-Level Token (xapp-...) |
| `GOAT_ADMIN_CHAT_ID.txt` | `GOAT_ADMIN_CHAT_ID` | Chat ID for admin alerts |

Env vars always win over creds files. Use `goated creds set KEY VALUE` to manage.

## Log structure

```
logs/
├── goat/                          # Daily CLI logs (YYYY-MM-DD.log)
├── goated_daemon.log              # Daemon stdout/stderr
├── message_logs/
��   ├── daily/YYYY-MM-DD.jsonl     # All messages by date
│   └── sessions/SESSION_ID.jsonl  # Per-session conversation
├── cron/
│   ├── runs.jsonl                 # Cron execution log
│   └── jobs/TIMESTAMP-cron-ID.log # Per-job output
└── subagent/
    └── jobs/TIMESTAMP.log         # Per-subagent output
```
