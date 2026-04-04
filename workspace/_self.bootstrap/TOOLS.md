---
title: Default Tools
kind: tools_guide
---

# TOOLS.md

This file explains the default tools available in this example `self/` repo and
how they should be used to advance missions.

## Core rule

- Use `MISSIONS/` for operational state and open loops.
- Use `VAULT/` for durable knowledge.
- Use `toolbox` and `notesmd` to do work, then write the results back into
  those files.

## Default tools

### `tools/toolbox`

Main personal CLI. Built from `tools/toolbox-cli/`.

Useful commands:

```bash
tools/toolbox remember "person or topic"
tools/toolbox notes search "query"
tools/toolbox browser run --task "do something on the web" --wait
tools/toolbox email check
tools/toolbox email send --to someone@example.com --subject "Hello" --body "Hi"
tools/toolbox voice say --text "hello" --no-catbox
```

### `tools/notesmd`

Bundled Obsidian-friendly markdown CLI.

Examples:

```bash
tools/notesmd list -v VAULT
tools/notesmd create "people/example-person" -v VAULT
tools/notesmd search-content -v VAULT "person name"
tools/notesmd print -v VAULT projects/example.md
```

## Mission advancement guidance

When a heartbeat or mission-oriented cron runs:

1. Read `MISSIONS/README.md`.
2. Inspect `MISSIONS/` for active or blocked missions.
3. Use the available tools to make the next concrete move.
4. Append execution details to the mission's `MISSION_LOG.md`.
5. Update `MISSION_TODO.md` so the next session knows what remains.
6. If durable knowledge was learned, update `VAULT/` as well.

## Durable knowledge vs execution state

Put things in `VAULT/` when they are:
- durable facts about people
- durable facts about projects or companies
- repeatable heuristics or patterns
- reference material likely to matter again

Put things in `MISSIONS/` when they are:
- next actions
- blockers
- progress updates
- work logs
- mission-specific notes that are still in motion
