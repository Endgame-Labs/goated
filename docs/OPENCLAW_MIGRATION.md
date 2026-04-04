# Migrating from OpenClaw to Goated

## Why migrate?

OpenClaw owns the context window. It injects bootstrap files, manages session history, and accumulates state in-process on every API request. This leads to:

- **Token bloat**: 3K-14K tokens of force-injected context per message, session history reprocessed every turn
- **Memory growth**: 1.9 GB after 13 hours, 28 GB by day three, eventually OOM-killed
- **Session fragility**: compaction can permanently lose SSH keys, credentials, and important context
- **Memory drift**: the agent's sense of identity and continuity degrades over long sessions

Goated takes a fundamentally different approach: it doesn't touch the context window at all. It's a ~20 MB daemon that routes messages and lets Claude Code (or Codex) manage its own context, compaction, and memory. See [PERFORMANCE.md](PERFORMANCE.md) for the full comparison.

## TL;DR -- the migration in 5 steps

```sh
# 1. Install Goated
git clone https://github.com/endgame-labs/goated.git
cd goated

# 2. Bootstrap a fresh agent
./bootstrap.sh

# 3. Copy your old OpenClaw workspace into the agent's private repo
cp -r /path/to/your/openclaw/workspace/* workspace/self/legacy_openclaw/

# 4. Start the daemon
./goated daemon run

# 5. Tell your agent to migrate itself
```

Send this message to your agent via Slack or Telegram:

> I'm migrating you from OpenClaw. All the old files are in `self/legacy_openclaw/`. Help me systematically migrate everything into your Goated world -- credentials, cron jobs, tools, memory, and any important context.

The agent will walk through the old files, set up credentials via `./goat creds set`, register cron jobs, and organize your data into the `workspace/self/` structure. It knows what goes where.

## What's different

| | OpenClaw | Goated |
|---|---|---|
| **Context management** | Framework injects bootstrap files + session history into every request | Framework doesn't touch the context window -- delegates to Claude Code/Codex |
| **Credentials** | Environment variables | File-backed (`workspace/creds/*.txt`) via `./goat creds` |
| **Cron jobs** | System crontab or external scheduler | Built-in cron runner with timezone support, dedup, and subagent spawning |
| **Agent messaging** | Delimiter-based output capture | Agent pipes markdown to `./goat send_user_message --chat <id>` |
| **Session management** | Manual restarts | Auto-healing with health checks, graceful drain, and restart |
| **Subagents** | Not supported | `./goat spawn-subagent --prompt "..."` with completion tracking |
| **Agent identity** | Bootstrap files injected per-request | Private `workspace/self/` Git repo the agent manages itself |
| **Chat gateway** | Built-in (Telegram) | Built-in (Slack and Telegram) |
| **Runtime** | OpenClaw's own runtime | Claude Code or Codex (TUI or headless) |
| **Daemon memory** | 1.9 GB+ growing unbounded | ~20 MB flat |

## Step-by-step migration

### 1. Install and bootstrap

```sh
git clone https://github.com/endgame-labs/goated.git
cd goated
./bootstrap.sh
```

Bootstrap will:
- Build both binaries (`goated` and `workspace/goat`)
- Initialize the database
- Create `workspace/self/` with starter templates (identity, memory, missions, vault, prompts)
- Set up two default cron jobs (hourly heartbeat, knowledge extraction every 8h)
- Walk you through gateway configuration (Slack or Telegram)

### 2. Copy your OpenClaw workspace

Copy your old OpenClaw workspace files into a `legacy_openclaw` folder inside the agent's private repo:

```sh
mkdir -p workspace/self/legacy_openclaw
cp -r /path/to/your/openclaw/workspace/* workspace/self/legacy_openclaw/
```

This preserves everything -- bootstrap files, tools, context files, session history -- without interfering with Goated's structure.

### 3. Migrate credentials

OpenClaw uses environment variables. Goated uses file-backed credentials.

**Manual migration:**

```sh
# For each secret your agent uses:
./goated creds set GITHUB_API_KEY ghp_xxxxxxxxxxxx
./goated creds set OPENAI_API_KEY sk-xxxxxxxxxxxx

# List stored credentials:
./goated creds list

# The agent reads them at runtime with:
./goat creds get <KEY_NAME>
```

**Or let the agent do it:** Once the daemon is running, tell the agent:

> Look through `self/legacy_openclaw/` for any API keys, tokens, or credentials. Migrate each one using `./goat creds set KEY VALUE`.

### 4. Migrate cron jobs

If you had system crontab entries for OpenClaw:

```
0 9 * * * cd /path/to/openclaw && claude -p "Check my email"
```

Replace with Goated's built-in cron:

```sh
# Subagent cron (spawns a headless agent with a prompt):
./goated cron add \
  --schedule "0 9 * * *" \
  --prompt "Check my email and summarize the important ones" \
  --chat <your_chat_id>

# Or use a prompt file for complex prompts:
./goated cron add \
  --schedule "0 9 * * *" \
  --prompt-file workspace/self/prompts/morning-email.md \
  --chat <your_chat_id>

# System cron (runs a shell command, no agent):
./goated cron add \
  --type system \
  --schedule "*/20 * * * *" \
  --command "bash workspace/self/scripts/sync-calendar.sh" \
  --silent
```

Prompt files are read at execution time, so you can edit them without re-registering the cron.

**Cron management:**

```sh
./goated cron list                        # List all crons
./goated cron disable <id>                # Pause without deleting
./goated cron enable <id>                 # Resume
./goated cron remove <id>                 # Delete
./goated cron set-schedule <id> "0 8 * * *"  # Change schedule
```

### 5. Configure your gateway

**Slack:**

Set up a Slack app with Socket Mode enabled, then:

```sh
./goated creds set GOAT_SLACK_BOT_TOKEN xoxb-your-bot-token
./goated creds set GOAT_SLACK_APP_TOKEN xapp-your-app-token
```

Set `"gateway": "slack"` and `"slack": {"channel_id": "YOUR_DM_CHANNEL_ID"}` in `goated.json`.

**Telegram:**

```sh
./goated creds set GOAT_TELEGRAM_BOT_TOKEN your-bot-token
```

Set `"gateway": "telegram"` in `goated.json`. Polling mode works out of the box; webhook mode requires a public URL.

### 6. Start the daemon

```sh
./goated daemon run
```

The daemon self-backgrounds. Logs go to `logs/goated_daemon.log`. Set up the watchdog cron for auto-recovery:

```sh
(crontab -l 2>/dev/null; echo '*/2 * * * * /path/to/goated/scripts/watchdog.sh') | crontab -
```

### 7. Let the agent finish the migration

Once the daemon is running, send your agent a message:

> I'm migrating you from OpenClaw. All the old files are in `self/legacy_openclaw/`. Help me systematically migrate stuff into your Goated world -- identity files, memory, tools, vault entries, and anything else worth keeping.

The agent understands the `workspace/self/` layout and will:
- Move identity/personality content into `self/IDENTITY.md` and `self/SOUL.md`
- Extract memory into `self/MEMORY.md`
- Organize knowledge into `self/VAULT/`
- Set up tools under `self/tools/`
- Migrate any prompt files into `self/prompts/`

Once everything useful has been migrated, you can delete `self/legacy_openclaw/`.

## The workspace/self pattern

This is the biggest conceptual difference from OpenClaw. In OpenClaw, the framework owns the agent's context -- it force-injects bootstrap files on every request. In Goated, the agent owns a private Git repository at `workspace/self/` and manages its own files:

```
workspace/self/
+-- CLAUDE.md          # Agent instructions (read on startup)
+-- IDENTITY.md        # Who the agent is
+-- MEMORY.md          # Long-term memory
+-- SOUL.md            # Values and voice
+-- USER.md            # Info about you
+-- VAULT/             # Durable knowledge (people, projects, patterns)
+-- MISSIONS/          # Operational work and plans
+-- prompts/           # Cron job prompt files
+-- tools/             # Custom CLI tools (Go, Python, etc.)
+-- creds/             # Credential files (gitignored from self repo too)
```

The agent reads what it needs, when it needs it. No force-injection, no token overhead for files the agent isn't using. And because it's a Git repo, you get full version history of your agent's evolving identity and knowledge.
