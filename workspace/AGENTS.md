---
title: Workspace Agent Guide
kind: workspace_instructions
---

# AGENTS.md

Read `GOATED.md` first for the shared operating contract, startup order,
authority boundaries, and reply transport. Read `self/AGENTS.md` for the
instance's identity, preferences, and operating references.

## Private state

All personal data belongs in the separate private `self/` repo:

- `IDENTITY.md`: name and stable identity.
- `SOUL.md`: concise values, voice, and judgment; change deliberately.
- `USER.md`: user profile and preferences.
- `MEMORY.md`: curated cross-session context, not a transcript.
- `GOALS/<slug>/GOAL.md`: outcomes, status, scope, and completion criteria.
- `MISSIONS/`: execution plans, TODOs, blockers, and work logs.
- `VAULT/`: sourced long-term knowledge about people, projects, and patterns.
- `HEARTBEAT.md`: hourly goal review and mission advancement.
- `prompts/dreaming.md`: eight-hour memory consolidation; reports in `dreams/`.

Use YAML frontmatter and links for discoverable, versioned Markdown. See
`../docs/KNOWLEDGE_FORMAT.md` for OKF-style examples and
`../docs/GOALS_AND_DREAMS.md` for lifecycle/scheduling details.
An optional private `goal_context` envelope is an index, not the full record:
read the relevant goal and reconcile it with the current request before acting.
Never disclose private goal or memory details merely because they are in context.

## Tools

Use `./goat` and `GOATED_CLI_README.md` for messaging, credentials, cron,
and tracked subagents. Use `./goat cron` for recurring work and
`./goat spawn-subagent` for delegated work, not runtime-native alternatives.
Credentials are managed through `./goat creds`; don't copy values into notes.

See `TOOLS.md` for building personal capabilities. Keep tools, projects, and
dependencies under `self/`; do not create package manifests, dependency trees,
or virtual environments in the shared workspace root. Prefer existing tools or
Go CLIs; isolate any necessary non-Go dependencies within their own tool folder.
