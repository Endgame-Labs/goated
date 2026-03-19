---
title: Missions System
kind: mission_index
---

# MISSIONS

This folder is for operational work that unfolds over time.

Each mission is a subfolder. A mission folder should contain markdown files with
YAML frontmatter so the state is machine-readable and easy to inspect in
Obsidian-style tools.

## Mission layout

Each mission should have at least:

- `MISSION.md` or `README.md` — mission definition
- `MISSION_LOG.md` — detailed execution log
- `MISSION_TODO.md` — open tasks, blockers, and next actions

## Suggested frontmatter for `MISSION.md`

```yaml
---
title: Mission Name
status: active
priority: medium
goal: One sentence explaining what success looks like
created_at: 2026-03-19
last_advanced_at: 2026-03-19T00:00:00Z
next_action: The single best next step
blockers: []
---
```

## Status meanings

- `active` — should be advanced during heartbeat if possible
- `blocked` — cannot advance without some dependency or decision
- `inactive` — not being pursued right now, but may be resumed later
- `done` — completed
- `archived` — no longer active, kept for history

## Mission lifecycle

Use these transitions:

- `active` -> `blocked` when the mission matters but cannot move until some
  dependency, decision, or input arrives
- `active` -> `inactive` when the mission is intentionally deprioritized or put
  on pause without being truly blocked
- `blocked` -> `active` when the blocker is cleared
- `inactive` -> `active` when the user asks to resume it or its reactivation
  condition is satisfied
- `active` -> `done` when the mission outcome is achieved
- `done` -> `archived` when you want to keep the history but remove it from
  operational focus

## When to use `inactive`

Mark a mission `inactive` when:
- the user explicitly deprioritizes it
- another mission should take precedence for now
- it should pause for later, but is not truly blocked
- onboarding is good enough for now and should stop steering every session

## How to mark a mission `inactive`

When changing a mission to `inactive`:

1. Update `MISSION.md` frontmatter:
   - set `status: inactive`
   - update `last_advanced_at`
   - set `next_action` to the condition for reactivation
2. Append a short entry to `MISSION_LOG.md` explaining:
   - why the mission became inactive
   - what should reactivate it
3. Update `MISSION_TODO.md`:
   - keep the remaining work visible
   - move deferred items into a clear "Resume later" section if helpful
4. Update any mission index note if needed so future sessions can see why the
   mission is paused

## Operating rules

- `MISSION_LOG.md` is the detailed timeline of what happened.
- `MISSION_TODO.md` is the source of truth for what still needs doing.
- Durable facts discovered while doing mission work should be promoted to
  `VAULT/`.
- If you learn something about the user or yourself while doing mission work,
  update `USER.md`, `IDENTITY.md`, `MEMORY.md`, or `SOUL.md` in the same loop.
- Missions should stay concrete. If a mission grows too broad, split it.
- Heartbeat should automatically advance only missions with `status: active`.
- Heartbeat should not automatically advance `inactive`, `done`, or `archived`
  missions.
- `inactive` missions should be resumed only when the user asks or when their
  reactivation condition is satisfied.

## Default mission

Fresh self repos start with `ONBOARD_USER/` as the first active mission.
Complete that mission before expanding into custom missions.
