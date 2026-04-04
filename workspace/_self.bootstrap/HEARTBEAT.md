---
title: Main Heartbeat
kind: heartbeat
schedule: hourly
timezone: America/Los_Angeles
---

# HEARTBEAT.md

This is the default recurring check-in for the private `self/` repo.

## What to do

1. Read `MISSIONS/README.md`.
2. Read `TOOLS.md` to see what capabilities are available.
3. Check `MISSIONS/ONBOARD_USER/` first. If onboarding is still active, advance
   that mission before anything else.
4. If onboarding is done or inactive, inspect `MISSIONS/` for another mission
   whose `status` is `active`.
5. Pick the best mission you can move forward with the tools you have.
6. Do a concrete piece of work.
7. Append what happened to that mission's `MISSION_LOG.md`.
8. Update that mission's `MISSION_TODO.md` with:
   - remaining work
   - blockers
   - the best next action
9. If you learned durable facts, update `VAULT/`.
10. If you learned something about the user or yourself, update `USER.md`,
    `IDENTITY.md`, `MEMORY.md`, or `SOUL.md` immediately before ending the loop.

## Rules

- Prefer advancing an existing mission over creating new mission sprawl.
- `ONBOARD_USER` is the default first mission in a fresh self repo.
- If a mission is blocked, record the blocker explicitly.
- Do not automatically advance missions whose status is `inactive`, `done`, or
  `archived`.
- Resume an `inactive` mission only if the user asks or the mission's
  reactivation condition is now satisfied.
- If there are no active missions, leave a short note in `MISSIONS/README.md`
  or the most appropriate mission index file explaining what is missing.
- Keep execution state in `MISSIONS/`.
- Keep durable knowledge in `VAULT/`.
