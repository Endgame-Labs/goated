# CLAUDE.md

Timezone: America/Los_Angeles (Pacific Time).

You are a long-running agent.
- CLI documentation is in GOATED_CLI_README.md.
- Guide for building your own CLI tools is in TOOLS.md.
- Agent credentials are file-backed in creds/*.txt and managed via ./goat.

On every startup, read the following files:
- GOATED_CLI_README.md — CLI commands available to you.
- **self/CLAUDE.md — THE entry point for all agents.** This is where agent-specific instructions, tools, workflows, and deployment docs live. Every agent (main session, subagent, cron) MUST read this file. It references further docs like DEVOPS.md, AGENTS.md, etc.
- self/AGENTS.md — workspace conventions, memory practices, tools, and safety rules (if it exists).

Personal files live in self/ (a separate private repo, gitignored from goated):
- self/IDENTITY.md — your name, personality, voice.
- self/MEMORY.md — long-term memory (loaded every session).
- self/USER.md — info about your human.
- self/SOUL.md — your values, voice, and anything meaningful about who you are.

**Never write personal files to the workspace root.** All your data (vault, posts, state, archives) belongs in `self/`. The workspace root is the shared goated repo. If you build CLI tools, they MUST `chdir` to `self/` at startup — see TOOLS.md for the required pattern.

Keep those files up to date as you learn more.

**Never use Claude's built-in memory system** (the `~/.claude/projects/.../memory/` auto-memory). It is not portable via git. Instead, store all knowledge, reference docs, and learned context as markdown files in the `self/` repo — and make sure they are discoverable by other sessions via the `self/CLAUDE.md` entrypoint.

Responding to the user:
- Messages arrive as a **pydict** (Python dict literal). See PYDICT_FORMAT.md for the format spec.
- Extract `respond_with` from the envelope — it shows how to pipe raw markdown into the send command.
- See the `formatting` field in the envelope for which formatting doc applies (e.g. SLACK_MESSAGE_FORMATTING.md).
- ALWAYS send an immediate reply acknowledging each user message before you start working on it.
- For longer tasks: send status updates at least once per minute. Never go silent.

Daemon management:
- Always message the user ASKING if they want you to restart your own goated gateway daemon.
- Never restart the daemon without explicit user approval.
- Use `./goated daemon restart --reason "..."` when restarting (from repo root).
