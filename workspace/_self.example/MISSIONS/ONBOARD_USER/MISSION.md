---
title: ONBOARD_USER
status: active
priority: high
goal: Establish a durable user profile, explain how this self repo works, and leave the user knowing how to extend it
created_at: 2026-03-19
last_advanced_at: 2026-03-19T00:00:00Z
next_action: Start a real onboarding conversation with the user and capture durable facts immediately
blockers: []
---

# ONBOARD_USER

This is the default first mission for a freshly bootstrapped `self/` repo.

This mission is mirrored by the `<onboarding>...</onboarding>` block in
`AGENTS.md`. While onboarding is active, that block should remain present. Once
onboarding is complete, mark this mission `inactive` and delete that XML block
from `AGENTS.md` in the same processing loop.

## Mission outcome

Success means:
- the user understands the basic `self/` layout
- the user understands the default scheduled jobs
- the user knows they can ask for new tools and new scheduled jobs in plain English
- `IDENTITY.md`, `USER.md`, and related files contain real durable facts
- the user's person note exists in `VAULT/people/`
- `USER.md` links to that person note

## Conversation goals

During onboarding, explain:
- what you can help with in plain language before explaining any internals
- ongoing work, saved notes, and recurring check-ins in plain language first
- `MISSIONS/`, `VAULT/`, and `HEARTBEAT.md` only after the plain-language
  explanation
- that `HEARTBEAT.md` and knowledge extraction are the only built-in scheduled
  jobs, if the user wants that level of detail
- if you use the word "cron", explain it as a recurring scheduled prompt in
  everyday language

Give concrete examples of extensibility, including examples like:
- "Make a new tool for checking my Gmail."
- "Let's set up your web browser. How do I give you a budget for that?"
- "Schedule a new job every weekday morning at 7am that does X."
- "List all crons."
- "List all scheduled jobs."

Also direct the user to give the agent its own email account. Recommend:
- AgentMail for a simple agent-specific inbox
- Gmail if the user prefers using an existing Google-based account

## Tone

- Assume the user is nontechnical unless they clearly show otherwise.
- Do not introduce yourself primarily as software infrastructure.
- Prefer short, helpful, everyday phrasing.
- Only introduce technical details when they are useful to the user.

## Required onboarding questions

Ask enough questions to populate:
- `USER.md`
- `IDENTITY.md`, if the user has preferences about your name, tone, or role
- `MEMORY.md`, if the user shares durable context worth loading every session

Useful topics:
- what the user wants help with first
- current missions and recurring responsibilities
- communication style and tone preferences
- timezone and schedule preferences
- whether they want browser automation
- whether they want an email inbox set up now
- whether they want additional scheduled jobs beyond the built-ins

## Required file updates

As soon as you learn durable facts:
- update `USER.md`, `IDENTITY.md`, `MEMORY.md`, or `SOUL.md` immediately
- do not wait until later in the session

Once you know enough about the user:
1. Use `tools/toolbox notes` to create a person note in `VAULT/people/`.
2. Write durable facts there.
3. Link that person note from `USER.md`.

Prefer one person note for the user, not duplicates.

## When to mark this mission inactive

Mark `ONBOARD_USER` as `inactive` once all of these are true:
- `USER.md` contains meaningful durable information
- `USER.md` links to the user's note in `VAULT/people/`
- the user has been told how `self/`, `MISSIONS/`, `VAULT/`, and `TOOLS.md`
  work
- the user has been told that `HEARTBEAT.md` and knowledge extraction are the
  built-in scheduled jobs
- any remaining optional setup work has been deferred into another mission or
  left as explicit TODOs

When marking it inactive:
- set `status: inactive`
- set `next_action` to the condition that would justify resuming onboarding
- write a short explanation in `MISSION_LOG.md`
- remove the `<onboarding>...</onboarding>` block from `AGENTS.md`
