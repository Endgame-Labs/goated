# Portable knowledge, goals, and tasks

Encourage **OKF-style Markdown** for long-term memory, TODOs, tasks, and goal
definitions. Goated already uses Markdown/YAML and linked notes; this guide
makes the convention explicit. It is inspired by the
[Open Knowledge Format specification](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md),
not a claim that Goated validates or implements the entire specification.

Use UTF-8 Markdown with YAML frontmatter containing `type` and a useful `title`.
Prefer ordinary relative Markdown links so notes work outside Obsidian too;
existing wikilinks can remain. Source references use `sources` entries with a
`resource`. Keep secrets out of both prose and metadata.
Goated's `status`, `summary`, `owner`, `next_action`, `blockers`, and
`last_reviewed_at` are local conventions, not required OKF fields.
Use ISO 8601 timestamps with timezone offsets for instants.

## Long-term memory

Keep `MEMORY.md` a concise map of current, durable facts and links into `VAULT/`.
Detailed notes should distinguish evidence, interpretation, and uncertainty:

```markdown
---
type: Note
title: Presentation preferences
sources:
  - resource: "../../MISSIONS/presentation/MISSION_LOG.md"
    description: User feedback recorded on October 4
last_reviewed_at: 2026-10-04T09:00:00-07:00
---

The user asked for slides with short headings and substantial speaker notes.
This supersedes the earlier draft's dense slide text.
Related: [Presentation goal](../../GOALS/presentation/GOAL.md).
```

Use the actual source, not an invented citation. Preserve source dates and
qualification when reconciling old claims. Edit the canonical fact rather than
accumulating contradictory copies. Dream reports link back to this evidence;
they do not become fresh corroboration.

## Goals, tasks, and TODOs

Use `GOALS/<slug>/GOAL.md` for outcomes and `MISSIONS/<slug>/MISSION.md` for
execution scope, with `MISSION_TODO.md` and `MISSION_LOG.md` alongside.
See [goal lifecycle and example](GOALS_AND_DREAMS.md).
Define success, owner, authority/scope, stop conditions, delivery destination,
next action, and dependencies. A file is not authorization.

```markdown
---
type: Task
title: Prepare conference slides
status: active
owner: agent
next_action: Draft an outline for review
blockers: []
---

Goal: [Conference presentation](../../GOALS/presentation/GOAL.md).
Success: User approves the deck and receives the final file.
Scope: Drafting only; no external submission.

- [x] Record the agreed audience and deadline.
- [ ] Draft the outline.
- [ ] Get user feedback.
- [ ] Deliver the final deck and verify the link.
```

Small tasks can live as checkboxes in a mission TODO. Larger tasks can have
their own linked Markdown files. Do not maintain competing copies of a task's
status in several lists: use links to its canonical definition.
Root TODO.md, if used, should be an index, not a second task database.

Use `active`, `blocked`, `inactive`, `done`, or `archived` consistently.
Record blockers and the next check for blocked work; preserve the resume
condition for paused work. Update goal summaries after verified progress,
not after every speculative thought. Keep detailed activity in mission logs.
