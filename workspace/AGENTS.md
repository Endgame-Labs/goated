# AGENTS.md

Timezone: America/Los_Angeles (Pacific Time).

This file is a compatibility entrypoint for Claude Code and Codex.

Read and follow `GOATED.md` first. That file is the shared runtime contract for
both Claude Code and Codex sessions in Goated.

You are a long-running agent.
- CLI documentation is in `GOATED_CLI_README.md`.
- Guide for building your own CLI tools is in `TOOLS.md`.
- Agent credentials are file-backed in `creds/*.txt` and managed via `./goat`.
- For any repeated/scheduled task, use `./goat cron ...` (or `./goated cron ...` from repo root). Do **not** use Codex or Claude built-in scheduling systems.

On every startup, read the following files:
- `GOATED_CLI_README.md` — CLI commands available to you.
- `self/CLAUDE.md` — THE entry point for all agents. This is where
  agent-specific instructions, tools, workflows, and deployment docs live.
  Every agent (main session, subagent, cron) MUST read this file. It
  references further docs like `DEVOPS.md`, `AGENTS.md`, etc.
- `self/AGENTS.md` — workspace conventions, memory practices, tools, and
  safety rules (if it exists).

Personal files live in `self/` (a separate private repo, gitignored from
goated):
- `self/IDENTITY.md` — your name, personality, voice.
- `self/MEMORY.md` — long-term memory (loaded every session).
- `self/USER.md` — info about your human.
- `self/SOUL.md` — your values, voice, and anything meaningful about who you
  are.

Never write personal files to the workspace root. All your data (vault, posts,
state, archives) belongs in `self/`. The workspace root is the shared goated
repo. If you build CLI tools, they MUST `chdir` to `self/` at startup; see
`TOOLS.md` for the required pattern.

Keep those files up to date as you learn more.

Never use Claude memory, Codex memory, etc for long-term knowledge and memory
state. The `self/` directory should handle all long-term state and portable
memory via git-backed markdown files. Check
`self/AGENTS.md`, `self/CLAUDE.md`, or `self/GEMINI.md` to learn more.

Responding to the user:
- Messages arrive in the configured transport envelope (currently pydict,
  Python dict literal). See `PYDICT_FORMAT.md` for the format spec.
- Extract `respond_with` from the envelope; it shows how to pipe raw markdown
  into the send command.
- See the `formatting` field in the envelope for which formatting doc applies
  (for example `SLACK_MESSAGE_FORMATTING.md`).
- ALWAYS send an immediate reply acknowledging each user message before you
  start working on it.
- For longer tasks: send status updates at least once per minute. Never go
  silent.

Daemon management:
- Always message the user asking if they want you to restart your own goated
  gateway daemon.
- Never restart the daemon without explicit user approval.
- Use `./goated daemon restart --reason "..."` when restarting (from repo
  root).
