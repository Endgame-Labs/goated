# Codebase — Code Map

For architecture overview, setup, and usage, see [README.md](README.md).
For build/run instructions, see [CLAUDE.md](CLAUDE.md) (aka AGENTS.md).

## Package reference

### `cmd/daemon/main.go` — Daemon entry point

Daemonizes via `nohup`, loads config, creates `TmuxBridge` + `Service`, initializes the
active connector (Slack or Telegram), runs the cron ticker (1min interval), handles
graceful shutdown with `Service.WaitInflight()`.

### `cmd/goated/cli/` — Cobra CLI commands

All commands are registered in `root.go::Execute()`. Both `./goated` and `./workspace/goat`
are built from the same source — the binary name determines which commands are available.

| File | Commands | Key functions |
|------|----------|---------------|
| `bootstrap.go` | `bootstrap` | Seeds `.env` from `.env.example`, creates DB, adds first channel, adds sync cron |
| `channel.go` | `channel list/add/switch/delete` | `writeChannelEnv()` updates `.env` when switching |
| `creds.go` | `creds set/get/list` | File-backed at `workspace/creds/<KEY>.txt` |
| `cron.go` | `cron run/add/list/enable/disable/remove/set-schedule/set-timezone/set-silent` | `cron run` is called by the daemon's ticker |
| `daemon.go` | `daemon restart/stop/status` | SIGTERM + wait, reads `logs/goated_daemon.pid` |
| `gateway.go` | `gateway telegram/slack` | Standalone connector runners |
| `send_user_message.go` | `send_user_message --chat` | Reads markdown from stdin, converts to platform format, posts. Manages Slack thinking indicator cleanup. Key functions: `sendViaSlack()`, `sendViaTelegram()`, `waitAndClearThinking()`, `postSlackThinking()` |
| `session.go` | `session restart/status/send` | `session send` pastes text directly into tmux pane |
| `spawn_subagent.go` | `spawn-subagent --prompt --chat` | Calls `subagent.RunBackground()` |
| `start.go` | `start` | Foreground alternative to daemon |
| `sync_self.go` | `sync_self_to_github` | Stages `.md` files in `self/`, checks for credential leaks, commits, pushes |
| `helpers.go` | — | `prompt()`, `withDefault()` utilities |

### `internal/app/config.go` — Configuration

- **`Config`** struct — all settings (workspace, DB, logs, tokens, gateway, timezone, admin chat)
- **`LoadConfig()`** — reads `.env` files (cwd, exe dir, exe parent dir), env vars override `.env`
- **`loadDotEnv(path)`** — parses `KEY=VALUE` lines, skips comments, doesn't overwrite existing env

### `internal/claude/tmux_bridge.go` — Claude Code session management

- **`TmuxBridge`** struct — holds `WorkspaceDir`, `LogDir`
- **`SendAndWait(ctx, channel, chatID, userPrompt, timeout)`** — wraps message in pydict envelope via `buildPromptEnvelope()`, pastes into tmux via `tmux.PasteAndEnter()`. NOTE: timeout param is currently ignored (`_`).
- **`IsSessionBusy(ctx)`** — delegates to `tmux.IsIdle()` (two captures 2s apart)
- **`waitForIdleOrStall(ctx, timeout)`** — polls pane every 3s, returns true if idle (stable + `❯`), false if stalled (30s unchanged without `❯`)
- **`EnsureSession(ctx)`** — creates `goat_main` tmux session running `claude --dangerously-skip-permissions`, waits for ready
- **`ClearSession(ctx, _)`** — kills session, calls `EnsureSession()`
- **`ContextUsagePercent(_)`** — pastes `/context`, polls for output, parses with `contextPctRe`
- **`SessionHealthy(ctx)`** — captures last 20 lines, matches against `healthErrorPatterns`
- **`RestartSession(ctx)`** / **`StopSession(ctx)`** — kill + restart or just kill
- **`SendRaw(ctx, text)`** — pastes text without envelope wrapping (used for `/compact`, `/clear`)
- **`buildPromptEnvelope(channel, chatID, userPrompt)`** — builds pydict with keys: message, source, chat_id, respond_with, formatting, instruction

### `internal/cron/runner.go` — Cron scheduler

- **`Runner`** struct — store, workspace dir, log dir, notifier
- **`Notifier`** interface — `SendMessage(ctx, chatID, text) error`
- **`Run(ctx, now)`** — iterates active crons, checks schedule against `now` in job's timezone, skips if previous run still in-flight. Dispatches to `runSubagentJob()` or `runSystemJob()`.
- Subagent jobs: call `subagent.RunSync()`, timeout 1 hour
- System jobs: `exec.CommandContext()` with 1 hour timeout

### `internal/db/db.go` — BoltDB persistence

Opened per-operation (no held locks) so daemon and CLI don't contend.

**Types:**
- **`Store`** — wraps `bbolt.DB` path
- **`CronJob`** — ID, Type (`subagent`/`system`), ChatID, Schedule, Prompt, PromptFile, Command, Timezone, Silent, Active
- **`CronRun`** — ID, CronID, RunMinute, Status, LogPath
- **`SubagentRun`** — ID, PID, Source, CronID, ChatID, Prompt, Status, LogPath, StartedAt, FinishedAt
- **`Channel`** — Name, Type (`telegram`/`slack`), Config map, CreatedAt

**Buckets:** `crons`, `cron_runs`, `subagent_runs`, `channels`, `meta`

**Key methods:** `AddCron()`, `ActiveCrons()`, `AllCrons()`, `GetCron()`, `SetCronActive()`, `DeleteCron()`, `RecordCronRun()`, `RecordSubagentStart()`, `RecordSubagentFinish()`, `RunningSubagents()`, `CronJobRunning()`, `GetMeta()`, `SetMeta()`, `AddChannel()`, `GetChannel()`, `AllChannels()`, `DeleteChannel()`

### `internal/gateway/` — Message routing

**`types.go`** — interfaces:
- **`IncomingMessage`** struct — Channel, ChatID, UserID, Text
- **`Handler`** interface — `HandleMessage(ctx, msg, responder) error`
- **`Responder`** interface — `SendMessage(ctx, chatID, text) error`
- **`Connector`** interface — `Run(ctx, handler) error`

**`service.go`** — central handler:
- **`Service`** struct — Bridge, Store, DefaultTimezone, AdminChatID, DrainCtx, inflight WaitGroup, compaction state (mutex, queue)
- **`HandleMessage(ctx, msg, responder)`** — routes `/clear`, `/chatid`, `/context`, `/schedule` commands. Otherwise: `ensureHealthySession()` (up to 5 retries), context check every 5 messages, `sendWithRetry()` (up to 2 retries on API errors)
- **`sendWithRetry(ctx, msg, responder)`** — calls `Bridge.SendAndWait()`, then checks pane for errors via `tmux.CheckPaneForError()`
- **`compactAndFlush(ctx, triggerMsg, responder)`** — sets `compacting=true`, waits for idle, sends `/compact`, waits for idle, flushes queued messages
- **`ensureHealthySession(ctx, responder, chatID)`** — calls `Bridge.SessionHealthy()`, retries with 1min backoff, DMs admin on failure
- **`WaitInflight()`** — blocks until all goroutines finish (used for graceful shutdown)

### `internal/pydict/` — Python dict literal codec

**`encode.go`:**
- **`KV`** struct — Key string, Value any
- **`Encode(map[string]any)`** — sorted keys, uses triple-quoted strings for multiline values
- **`EncodeOrdered([]KV)`** — preserves key order

**`parse.go`:**
- **`Parse(input)`** — tokenizer + recursive descent parser, handles triple-quoted strings, single/double quotes, booleans, None, nested dicts/lists

**`pydict_test.go`** — round-trip and edge case tests

### `internal/slack/connector.go` — Slack Socket Mode

- **`Connector`** struct — api client, socket client, store, channelID, thinkingTS (mutex-guarded), seenEvents map (dedup, currently unbounded)
- **`NewConnector(botToken, appToken, channelID, store)`**
- **`Run(ctx, handler)`** — spawns goroutine processing `socket.Events`, filters for MessageEvent, deduplicates, posts thinking indicator, calls `handler.HandleMessage()`
- **`SendMessage(ctx, channelID, text)`** — converts markdown to Slack mrkdwn via `util.MarkdownToSlackMrkdwn()`, chunks at 4000 chars
- **`postThinking(channel)`** — posts `_thinking..._`, writes timestamp to `/tmp/goated-slack-thinking`, spawns `reapThinkingIndicator()` goroutine
- **`clearThinkingIfNeeded(channel)`** — reads thinkingTS, deletes file + Slack message
- **`ReapThinkingIndicator(api, channel, ts)`** — exported TTL safety net: 4min soft deadline (deletes if idle), 20min hard deadline (deletes unconditionally)
- **`ThinkingFile`** constant — `/tmp/goated-slack-thinking`

### `internal/subagent/run.go` — Headless subagent execution

- **`RunOpts`** struct — WorkspaceDir, Prompt, LogPath, Source, CronID, ChatID, Silent
- **`BuildPrompt(preamble, userPrompt, chatID, source, logPath)`** — constructs prompt with send command and formatting instructions
- **`RunSync(ctx, store, opts)`** — runs `claude -p` synchronously, captures output, records in DB
- **`RunBackground(store, opts)`** — starts `claude -p` in background goroutine, returns PID

### `internal/telegram/connector.go` — Telegram bot

- **`Connector`** struct — bot client, store
- **`RunMode`** type — `"polling"` or `"webhook"`
- **`WebhookOptions`** struct — URL, ListenAddr, Path
- **`NewConnector(token, store)`**
- **`Run(ctx, handler, mode, webhookOpts)`** — dispatches to `runPolling()` or `runWebhook()`
- **`runPolling(ctx, handler)`** — long-polls with `bot.GetUpdatesChan()`, persists offset in DB, spawns typing indicator loop per message
- **`runWebhook(ctx, handler, opts)`** — sets webhook, listens on HTTP, same handler flow
- **`SendMessage(ctx, chatID, text)`** — converts markdown to Telegram HTML via `util.MarkdownToTelegramHTML()`, falls back to plain text if HTML rejected

### `internal/tmux/tmux.go` — Tmux primitives

- **`SessionExists(ctx)`** — `tmux has-session -t goat_main`
- **`PasteAndEnter(ctx, text)`** — writes text to temp file, `tmux load-buffer` + `paste-buffer`, polls until prompt line changes (5s timeout), sends Enter key
- **`CapturePane(ctx)`** — full scrollback of `goat_main:0.0` (`-p -S -`)
- **`CaptureVisible(ctx)`** — visible portion only (`-p`)
- **`Run(ctx, args...)`** / **`RunOutput(ctx, args...)`** — execute arbitrary tmux commands
- **`WaitForIdle(ctx, timeout)`** — captures every 2s, requires 2 consecutive unchanged captures + `HasPrompt()` to return nil
- **`IsIdle(ctx)`** — quick version: two captures 2s apart, returns `snap1 == snap2 && HasPrompt(snap2)`
- **`HasPrompt(paneOutput)`** — checks last 5 lines for `❯`
- **`CheckPaneForError(ctx)`** — scans last 15 lines against `retryableErrors` list (API 5xx, overloaded, internal server error)

### `internal/util/` — Format conversion

**`slackformat.go`:**
- **`MarkdownToSlackMrkdwn(md)`** — line-by-line conversion: headers→bold, fenced code blocks pass through, inline bold/italic/strike/code, blockquotes, lists. Strips markdown backslash escapes (`\!`, `\.`, `\-`, etc.)

**`telegramhtml.go`:**
- **`MarkdownToTelegramHTML(md)`** — converts to `<b>`, `<i>`, `<s>`, `<code>`, `<pre>`, `<blockquote>`. HTML-escapes content. Handles language-tagged code blocks.

**`sanitize.go`:**
- **`SafeName(in)`** — replaces non-alphanumeric chars with `_`

**`text.go`:**
- **`ExtractUserMessage(s)`** — parses `:START_USER_MESSAGE:...:END_USER_MESSAGE:` blocks from pane output, handles ANSI codes and TUI artifacts

## Package dependency graph

```
cmd/daemon/main.go
├── internal/app          (config)
├── internal/db           (store)
├── internal/claude       (TmuxBridge)
├── internal/gateway      (Service)
├── internal/cron         (Runner)
├── internal/slack        (Connector)
└── internal/telegram     (Connector)

internal/gateway/service.go
├── internal/claude       (Bridge interface — TmuxBridge)
├── internal/db           (Store — cron, meta)
└── internal/tmux         (CheckPaneForError)

internal/claude/tmux_bridge.go
├── internal/tmux         (PasteAndEnter, CaptureVisible, IsIdle, WaitForIdle, HasPrompt, etc.)
└── internal/pydict       (EncodeOrdered — prompt envelope)

cmd/goated/cli/send_user_message.go
├── internal/slack        (ThinkingFile, ReapThinkingIndicator)
├── internal/tmux         (WaitForIdle, IsIdle, CaptureVisible)
└── internal/util         (MarkdownToSlackMrkdwn, MarkdownToTelegramHTML)

internal/cron/runner.go
└── internal/subagent     (RunSync)
    └── internal/db       (RecordSubagentStart/Finish)
```
