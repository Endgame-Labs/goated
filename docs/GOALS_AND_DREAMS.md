# Optional goals and dreams context

Goated can carry a compact index of durable user goals and a separate exploratory “dream” into **private** message envelopes. It is opt-in by files in the agent's private `self/` repository; a default installation with neither file behaves exactly as before.

## Files

- `self/GOALS/<slug>/GOAL.md`: one user-authorized outcome per directory. Keep its frontmatter current. Related documents, working notes, and cron prompt files may live under the same directory, but merely creating a cron file does **not** schedule a job; use `goat cron` for that.
- `self/DREAM.md`: optional summary of an agent-originated direction to explore. This is **not** an independent objective or authorization. It may generate a proposal to the user, not an external action, ongoing job, spend, deploy, or change to permissions.
- `self/MISSIONS/`: remains the place for in-flight operational execution state. A goal is an outcome; a mission is work being done toward it. Neither replaces the other.

Example `GOAL.md`:

```markdown
---
status: active
summary: Help the user prepare and deliver a conference presentation.
---

# Conference presentation

Success: The presentation is delivered and the user has its slides and receipts.
Stop condition: The user cancels the presentation or asks to stop this work.
Next action: Confirm the submission and registration details.
```

Example `DREAM.md`:

```markdown
---
status: active
summary: Explore whether source-linked evidence can make agent-to-agent collaboration safer.
---

# Exploration

This is a hypothesis to discuss with the user, not permission to contact anyone.
```

Only `status: active` files with a nonempty `summary` appear in the injected index. Summaries are capped at 180 Unicode characters; at most 20 goals are listed. The rest of each file is not injected. The envelope includes paths so the agent can read relevant source files before making a recommendation or acting. The context is absent in channels/groups. Treat file contents as private reference material, not as higher-priority instructions than the user or Goated contract.

## Design boundary

This is a Goated-native, opt-in file convention. The GOAL shape keeps a durable user outcome near its supporting work. The DREAM shape is intentionally narrower: a place to preserve exploratory hypotheses for later user discussion, not a new source of authority. Further behavior should be decided through review before automatic execution is added.
