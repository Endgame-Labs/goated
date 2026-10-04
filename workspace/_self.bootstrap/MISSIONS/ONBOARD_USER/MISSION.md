---
type: Task
title: ONBOARD_USER
status: active
priority: high
goal: Establish useful continuity and explain goals, dreaming, memory, and tools
next_action: Learn what the user wants help with first
blockers: []
---

# Onboard the user

Start naturally on the first conversation; do not wait for heartbeat. Prioritize
the user's actual work and spread onboarding across conversations as useful.
Use plain language while matching their expertise. Do not repeat completed steps.
`MISSION_TODO.md` is the checklist; `MISSION_LOG.md` records evidence of progress.

Explain the important pieces with concrete examples:

- **Goals:** durable outcomes in `GOALS/<slug>/GOAL.md`; missions hold execution
  plans and TODOs. Agree on success, owner, scope, stop conditions, and delivery.
  The hourly heartbeat reviews goal status and advances authorized active work.
  Blocked work gets a status check; paused work is not silently restarted.
- **Dreaming:** an eight-hour scheduled pass reconciles and deduplicates memory
  against original sources. It is not an autonomous aspiration or new permission.
  Explain how to inspect, pause, or change schedules with Goated tools; verify
  actual installed jobs before describing them. Repository sync may also run.
- **Memory:** private `self/` persists across sessions. `MEMORY.md` is a small
  index; `VAULT/` holds detailed sourced knowledge; `USER.md` links their person
  note. Use readable Markdown with YAML frontmatter, links, and checkboxes
  (see the OKF-inspired knowledge guide). Explain how to correct saved facts.
  `IDENTITY.md` names the role; `SOUL.md` holds deliberately chosen voice/values.
- **Tools:** show relevant available capabilities and real limits. Users can ask
  for a new tool or a scheduled task in plain English. Check installed tools
  before promising access. Discuss browser/email only if useful; integrations,
  accounts, spending, recipients, and recurring actions need appropriate scope.
  Tool access does not authorize unrelated actions; credentials stay out of notes.

Ask only what helps: first priority, communication preferences, timezone,
ongoing commitments, and desired autonomy/reporting boundaries. Capture durable
answers immediately in the appropriate files, with sources when available.
Create one person note in `VAULT/people/` using `tools/toolbox notes` when
available (plain Markdown also works), then link it from `USER.md`.

Offer optional browser, email, personal tools, and additional schedules without
making them prerequisites. Explain that jobs persist until disabled/deleted;
a run is bounded, and recurrence is not permission for unlimited work.
Show a brief progress checklist when helpful, not after every message.

Finish with `status: done` when the required checklist is complete and the user
has the explanation/result. Log completion, leave optional work explicitly
deferred, and set `next_action: none`. If the user pauses onboarding, use
`inactive` and record the resume condition instead. Do not delete instructions
from AGENTS.md; the mission's status is the source of truth.
