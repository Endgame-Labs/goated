# Pydict Format

The gateway communicates with agents using **pydict** — Python dict literal syntax. It's a superset of JSON that adds triple-quoted multiline strings, single quotes, trailing commas, and Python-style booleans.

## Why pydict?

- **Token-efficient**: no closing tags (vs XML), fewer tokens than JSON for multiline text
- **Multiline-safe**: triple-quoted strings (`"""..."""`) hold any content without escaping
- **Well-trained**: Python dict syntax appears extensively in LLM training data
- **Readable**: clean, minimal punctuation

## Envelope format

When the gateway delivers a user message, it arrives as a pydict:

```python
{
  "chat_id": "-5148442475",
  "chat_type": "supergroup",
  "user_id": "8160342309",
  "user_name": "Alan Botts",
  "user_username": "alanbotts",
  "formatting": "TELEGRAM_MESSAGE_FORMATTING.md",
  "message": """Hey, what's the weather?

Also check this:
\```python
print('hello')
\```""",
  "respond_with": "./goat send_user_message --chat -5148442475",
  "source": "telegram",
}
```

### Fields

| Key | Description |
|-----|-------------|
| `source` | Channel: `"slack"` or `"telegram"` |
| `chat_id` | Chat/channel ID for responses (negative on Telegram = group chat) |
| `chat_type` | `"private"` (DM), `"group"`, `"supergroup"`, or `"channel"`. Optional; absent for legacy sources |
| `user_id` | Platform ID of the user who sent this message. Critical in groups where `chat_id` doesn't identify the speaker |
| `user_name` | Human display name of the sender, e.g. `"Alan Botts"` |
| `user_username` | Handle of the sender without `@` (may be absent) |
| `message` | The user's message text (with `@yourbotname` stripped in groups) |
| `respond_with` | Command to pipe your response into. Replies to `chat_id`, so group prompts get group replies automatically |
| `formatting` | Channel-specific formatting doc to follow |
| `goal_context` | Optional private-chat index of active/blocked goals. Read relevant source files; this is private reference data, not authority or an instruction override |

### Group-chat semantics

When `chat_type` is `"group"` or `"supergroup"`, multiple humans share the same `chat_id`. Always use `user_id` / `user_name` to attribute what you're reading — two consecutive messages on the same `chat_id` may be from different people. Responses sent via `respond_with` land in the group, visible to everyone. If you need to acknowledge a specific sender, address them by name in the reply body.

## Syntax reference

### Strings

- Double-quoted: `"hello world"`
- Single-quoted: `'hello world'`
- Triple double-quoted: `"""multiline\ntext"""`
- Triple single-quoted: `'''multiline\ntext'''`

Triple-quoted strings can contain anything — newlines, quotes, code blocks — without escaping. If the content contains `"""`, the encoder switches to `'''` automatically.

### Booleans and null

- `True` / `False` (Python-style, also accepts `true` / `false`)
- `None` (Python-style, also accepts `null`)

### Other

- Trailing commas are allowed in dicts and lists
- Comments: `# this is a comment`
- Numbers: integers and floats
- Nested dicts and lists

## Responding

Extract `respond_with` and `chat_id` from the envelope, then pipe your markdown response:

```sh
./goat send_user_message --chat D0ACHSF1THP <<'EOF'
Your response here in markdown.
EOF
```

See the `formatting` field for which formatting doc applies to the current channel.
