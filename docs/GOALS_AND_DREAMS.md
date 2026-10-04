# Goals, heartbeat, and dreaming

Goals preserve user-authorized outcomes. Heartbeat advances work and reviews
goal status. Dreaming consolidates memory. None grants additional authority.

## Goals

Use `self/GOALS/<slug>/GOAL.md` for a durable outcome and link its execution
missions in `self/MISSIONS/`. Use one canonical TODO list per mission.
Follow [the OKF-inspired Markdown convention](KNOWLEDGE_FORMAT.md).

```markdown
---
type: Goal
title: Conference presentation
status: active
summary: Prepare and deliver the conference presentation.
owner: user
last_reviewed_at: 2026-10-04T09:00:00-07:00
next_action: Confirm the submission deadline
blockers: []
---

# Conference presentation
Success: Talk delivered and final slides handed to the user.
Scope: Draft and rehearse; ask before purchases or external submissions.
Stop: User cancels or asks to pause.
Delivery: Original private conversation, with a link to the final slides.
Execution: [Presentation mission](../../MISSIONS/presentation/MISSION.md).
```

Statuses are `active`, `blocked`, `inactive` (paused), `done`, and `archived`.
Hourly heartbeat reviews active/blocked goals, verifies evidence, and updates
status, blockers, next action, and review time. Completion requires the outcome
and delivery, not merely activity. Respect pause/cancel and goal scope.

Private message envelopes contain an optional `goal_context` index of active
and blocked goals with nonempty summaries (at most 20, alphabetical order,
180 Unicode characters plus a truncation marker per summary). Full bodies are
not injected. Read relevant files before acting. This index is absent in groups
and channels; it is reference material, not higher-priority instructions.
Do not disclose private goal details just because they are accessible.
These prompting rules are not an access-control sandbox. Tool permissions and
runtime controls still enforce the actual boundaries; group omission applies
to this index, not a blanket guarantee about every memory provider.

## Dreaming

The eight-hour agent job runs `self/prompts/dreaming.md` on
`0 */8 * * *` in the configured timezone. It reconciles source-backed durable
memory, deduplicates notes, flags contradictions, and records coverage/checkpoints
under `self/dreams/`. Reports are an audit trail, not independent evidence.
Normal work still saves important facts immediately. Dreaming does not rewrite
SOUL, invent objectives, restart paused work, or contact people.

There is no special aspiration file: legacy `self/DREAM.md` is not injected.
If it contains useful ideas, review them with the user and put authorized work
in goals, or preserve hypotheses as explicitly labeled notes.

## Installation and upgrades

Fresh bootstrap installs hourly heartbeat and eight-hour dreaming subagent jobs.
Inspect actual jobs with `./goat cron list`; repository sync may also be installed.
Cron configuration is authoritative; a frontmatter schedule is descriptive only.

Existing private self repos are never overwritten. Bootstrap recognizes the
legacy `self/prompts/knowledge_extraction.md` job as the dreaming slot, including
disabled jobs, preserving its ID, schedule, notifications, and custom prompt.
It does not add a competing job. If the new prompt is absent, bootstrap skips
creating a broken job and tells you to install/review the private prompt.

To upgrade an existing instance, review the new seed instructions against your
private customizations. Adapt the dreaming instructions into the existing
knowledge-extraction prompt, or install `prompts/dreaming.md` and make the old
prompt delegate to it. Keep the existing job and verify its configuration;
do not schedule both entrypoints. The seed legacy entrypoint is a forwarding
file for this purpose. Also reconcile heartbeat/onboarding/AGENTS changes
manually. No daemon restart or private-repo migration happens through this PR.
