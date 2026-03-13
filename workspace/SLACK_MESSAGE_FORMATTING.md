# Slack Message Formatting

Your markdown is auto-converted to Slack's mrkdwn format by `send_user_message`. Write standard markdown — the conversion handles the rest.

## What works

| Markdown you write       | Slack renders as       |
|--------------------------|------------------------|
| `**bold**`               | *bold*                 |
| `*italic*`               | _italic_               |
| `***bold italic***`      | *_bold italic_*        |
| `~~strikethrough~~`      | ~strikethrough~        |
| `` `inline code` ``      | `inline code`          |
| ` ```code blocks``` `    | code blocks            |
| `> blockquote`           | blockquote             |
| `# Heading`              | rendered as **bold**   |
| `- item` / `* item`     | bullet list            |

## What does NOT work in Slack

- **Markdown tables** — Slack has no table support. Always wrap tabular data in fenced code blocks:

```
\`\`\`
#  | Name         | Value
---|--------------|--------
1  | Acme Corp    | $150K
2  | Initech      | $95K
\`\`\`
```

- **Nested formatting** — Slack mrkdwn doesn't nest well (e.g. bold inside italic)
- **Links** — `[text](url)` is NOT converted; use raw URLs or Slack's `<url|text>` syntax
- **Images** — not supported inline; Slack will unfurl URLs that point to images
- **Ordered lists** (`1. item`) — no native support; use unordered lists or code blocks

## Message size limit

Slack caps messages at **4,000 characters**. Longer messages are automatically split at newline boundaries. Keep this in mind for large outputs — prefer concise responses or split across multiple `send_user_message` calls.

## Attachment ingest notes

- Incoming Slack attachment paths are passed as workspace-relative paths (for example `workspace/tmp/slack/attachments/...`).
- The envelope always includes `attachments_failed` with machine-readable `reason_code` values.
- Current `reason_code` values:
  - `unsupported_type`
  - `too_large`
  - `download_failed`
  - `corrupt`
  - `unauthorized`
  - `deduped`
