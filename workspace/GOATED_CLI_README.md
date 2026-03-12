# GOATED_CLI_README.md

`./goat` is the agent-facing CLI.

## Sending messages to the user

Use `send_user_message` to respond to the user via Telegram:

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

### Supported markdown formatting

- **Bold**: `**text**`
- *Italic*: `*text*`
- ***Bold italic***: `***text***`
- ~~Strikethrough~~: `~~text~~`
- `Inline code`: `` `code` ``
- Fenced code blocks with language tags: ` ```python ... ``` `
- Blockquotes: `> text`
- Headers (rendered as bold): `# Heading`
- Lists: `- item` or `* item`

### Slack formatting tips

When sending messages via Slack, tables and data visualizations render much better inside **code blocks** than as raw markdown tables. Slack doesn't support markdown table syntax natively, so use fenced code blocks for any tabular data:

```
\`\`\`
#  | Account      | Value   | Stage
---|--------------|---------|----------
1  | Acme Corp    | $150K   | Renewal
2  | Initech      | $95K    | Discovery
\`\`\`
```

This applies to any structured data: reports, comparisons, dashboards, lists with columns. Plain markdown tables will render as messy text in Slack — always wrap them in code blocks.

### Optional flags

- `--source <name>` — caller source (e.g. `cron`, `subagent`). When set, the message is also shared with the main interactive session for context.
- `--log <path>` — path to the caller's log file. Included in the main session notification so the user can inspect it.

These flags are typically included in the `send_user_message` command provided in your prompt. Use them as given.

### Important notes

- The `--chat` flag is required. Your chat ID is provided in the prompt envelope.
- Message is read from **stdin** — always pipe or heredoc your content.

## Credential management

- Set secret: `./goat creds set GITHUB_API_KEY ghp_xxx`
- Read secret: `./goat creds get GITHUB_API_KEY`
- List secrets: `./goat creds list`

## Cron management

- Add cron (inline): `./goat cron add --chat <chat_id> --schedule "0 8 * * *" --prompt "Send me Berkeley weather"`
- Add cron (file): `./goat cron add --chat <chat_id> --schedule "0 8 * * *" --prompt-file /path/to/prompt.md`
- List crons: `./goat cron list --chat <chat_id>`
- Disable cron: `./goat cron disable <id>`
- Enable cron: `./goat cron enable <id>`
- Remove cron: `./goat cron remove <id>`

## Subagents

- Run headless helper: `./goat spawn-subagent --prompt "Run a headless task"`

## Control-plane CLI

The control-plane CLI is `./goated` (from the repo root).

### Session management

```sh
./goated session status                        # Health, busy state, context estimate
./goated session restart                       # Kill and restart the Claude tmux session
./goated session send /context                 # Send a slash command or text to Claude
./goated session send "What are you working on?"
```

`session send` pastes text directly into the Claude Code tmux pane and presses Enter. Useful for sending Claude Code slash commands (`/context`, `/clear`) or ad-hoc prompts without going through the gateway.
