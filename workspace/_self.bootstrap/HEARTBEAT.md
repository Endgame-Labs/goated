---
title: Heartbeat
type: Playbook
kind: heartbeat
schedule: hourly
---

# Heartbeat

Run once, then finish. Resolve paths relative to `self/`. Follow the shared
Goated contract and the user's recorded scope, pauses, and stop conditions.

1. Read `GOALS/README.md` and each ongoing goal's `GOAL.md` (active or
   blocked). Reconcile recent user decisions and mission evidence. Update
   status, progress, blockers, next action, and `last_reviewed_at`. Mark done
   only when success criteria are verified; record the evidence. Do not restart
   inactive, done, or archived goals without an applicable user instruction.
2. Read `MISSIONS/README.md` and `TOOLS.md`. If onboarding is active, advance
   its next useful step while respecting the user's current priorities.
3. Otherwise choose one useful step in an active mission supporting an active
   goal, or another explicitly authorized standalone mission. Check blocked
   work for changed dependencies. Missing authorization is a blocker, not a
   reason to invent new permissions.
4. Do the step, verify its result, and update `MISSION_LOG.md`,
   `MISSION_TODO.md`, and the linked goal. Preserve other open commitments.
   If the task produced a requested deliverable, send it through the configured
   destination; recording completion in a file is not delivery.
5. Reconcile durable facts in `VAULT/`, `USER.md`, or `MEMORY.md` as needed.
   Report meaningful progress, a new blocker, or a requested result. Stay quiet
   for unchanged/no-op checks unless the user requested routine reports.

If no work is actionable, record the blocker or missing next step in the
appropriate goal/mission rather than generating busywork. Only reactivate
paused work when the user asks or its explicit reactivation condition holds.
