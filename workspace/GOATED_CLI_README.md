# GOATED_CLI_README.md

`./goat` is the agent-facing CLI.

## Sending messages to the user

Use `send_user_message` to respond to the user through the active gateway:

```sh
echo "Your markdown message" | ./goat send_user_message --chat <chat_id>
```

Or with a heredoc for multi-line messages:

```sh
./goat send_user_message --chat <chat_id> <<'EOF'
Here is a **bold** and *italic* example.

```python
print("code blocks work too")
```

And `inline code` as well.
EOF
```

### Message formatting

Write standard markdown. The CLI auto-converts it for the active gateway. For channel-specific details (what works, what doesn't, size limits):

- **Slack**: see [SLACK_MESSAGE_FORMATTING.md](SLACK_MESSAGE_FORMATTING.md)
- **Telegram**: see [TELEGRAM_MESSAGE_FORMATTING.md](TELEGRAM_MESSAGE_FORMATTING.md)

### Optional flags

- `--source <name>` — caller source (e.g. `cron`, `subagent`). When set, the message is also shared with the main interactive session for context.
- `--log <path>` — path to the caller's log file. Included in the main session notification so the user can inspect it.

These flags are typically included in the `send_user_message` command provided in your prompt. Use them as given.

Use `send_user_file` to send a local file or screenshot back to the user:

```sh
./goat send_user_file --chat <chat_id> --path /tmp/screenshot.png
./goat send_user_file --chat <chat_id> --path /tmp/report.pdf --caption "Latest report"
./goat send_user_file --chat <chat_id> --path /tmp/screenshot.png --type photo
```

### `send_user_file` notes

- `--path` must be a local file path on disk.
- `--type auto` chooses `photo` for common image formats and `document` otherwise.
- `--caption` is optional.
- Media delivery currently depends on gateway support; Telegram supports this path.

### Important notes

- The `--chat` flag is required. Your chat ID is provided in the prompt envelope.
- Message is read from **stdin** — always pipe or heredoc your content.

## Credential management

- Set secret: `./goat creds set GITHUB_API_KEY ghp_xxx`
- Read secret: `./goat creds get GITHUB_API_KEY`
- List secrets: `./goat creds list`

## Slack history

Use `./goat slack history` when you need to inspect prior Slack messages without dropping to raw `curl`.

```sh
./goat slack history
./goat slack history --limit 50 --reverse
./goat slack history --latest 1774060301.552199 --limit 10
./goat slack history --oldest 1774060301.552199 --inclusive --limit 20
./goat slack history --cursor dXNlcjpVMEc5V0ZYTlo=
./goat slack history --json | jq '.messages[].text'
```

Notes:

- `--chat <id>` overrides the configured Slack DM/channel ID. If omitted, the command uses `GOAT_SLACK_CHANNEL_ID`.
- `--latest` and `--oldest` accept raw Slack timestamps like `1774060301.552199`.
- `--reverse` prints messages oldest-first, which is useful for playback/reconstruction.
- Use `--limit`, `--latest`, and `--oldest` together to replay a bounded window of older messages around a known event.
- Use `--cursor` to keep paging backward when Slack returns `response_metadata.next_cursor`.
- `--json` returns the raw Slack API payload, including `response_metadata.next_cursor` for pagination.
- This command requires `GOAT_SLACK_BOT_TOKEN`.

## Cron management

Use Goated cron for all recurring work. Do **not** use any runtime-native scheduling systems.

- Add cron (inline): `./goat cron add --chat <chat_id> --schedule "0 8 * * *" --prompt "Send me Berkeley weather"`
- Add cron (file): `./goat cron add --chat <chat_id> --schedule "0 8 * * *" --prompt-file /path/to/prompt.md`
- List crons: `./goat cron list --chat <chat_id>`
- Disable cron: `./goat cron disable <id>`
- Enable cron: `./goat cron enable <id>`
- Remove cron: `./goat cron remove <id>`

## Subagents

- Run headless helper: `./goat spawn-subagent --prompt "Run a headless task"`
- `spawn-subagent` automatically prepends workspace instructions telling the subagent to read `self/AGENTS.md` first.

## Session management

```sh
./goat session status                        # Health, busy state, context estimate
./goat session restart                       # Restart the active session and preserve conversation when supported
./goat session restart --clear               # Discard prior conversation and start fresh
./goat session send /context                 # Send a slash command or text to the active runtime
./goat session send "What are you working on?"
```

`session send` pastes text directly into the active runtime tmux pane and
presses Enter. Useful for sending slash commands (`/context`, `/clear`) or
ad-hoc prompts without going through the gateway.

## Standard memory search

When `memory.search_command` is configured in `goated.json`, use `./goat memory search "query"` to run the configured provider and receive JSON chunks with `source` and `text`. The gateway also runs this provider automatically before each normal user turn. The provider is an argv array (no shell interpolation); `{query}` is replaced with the current user message, or the query is appended if no placeholder appears. An example Alan-specific setting is `["self/tools/alan", "remember", "{query}"]`.
