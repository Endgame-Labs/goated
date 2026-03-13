# GOATED.md

Timezone: America/Los_Angeles (Pacific Time).

This file is the shared runtime contract for Goated. It applies to the main
interactive session, headless subagents, and cron runs, regardless of whether
the active runtime is Claude Code or Codex.

On every startup, read these files in order:
- `GOATED_CLI_README.md` for the agent-facing CLI contract.
- `self/CLAUDE.md` as the private agent entrypoint. This is the source of your
  personal instructions, workflows, and references.
- `self/AGENTS.md` if it exists. When both files exist, `self/AGENTS.md` takes
  precedence over `self/CLAUDE.md` for workspace conventions and safety rules.

Your private state lives under `self/`, which should be a separate private repo.
Never write personal notes, memory, vault data, or projects into the workspace
root. If you build tools, make them `chdir` into `self/` at startup unless the
tool is explicitly for the shared Goated repo.

Do not rely on runtime-managed memory systems. Store durable knowledge in the
`self/` repo as markdown that other sessions can discover through
`self/CLAUDE.md` or `self/AGENTS.md`.

Responding to the user:
- Messages arrive as a pydict envelope. See `PYDICT_FORMAT.md`.
- Extract `respond_with`, `chat_id`, and `formatting` from the envelope.
- Send replies by piping markdown into the provided `respond_with` command.
- Use the formatting doc named in the envelope:
  - `SLACK_MESSAGE_FORMATTING.md`
  - `TELEGRAM_MESSAGE_FORMATTING.md`
- Always send an immediate acknowledgement for each user message.
- For longer tasks, send status updates at least once per minute.

Daemon management:
- Never restart the Goated daemon without explicit user approval.
- If a restart is needed, ask first and use `./goated daemon restart --reason "..."`
  from the repo root.

Instruction precedence:
- This runtime contract governs message handling and reply behavior for Goated.
- Repo-root instructions from parent directories may still be visible to the
  runtime, but Goated-specific reply and tool behavior is defined here.
