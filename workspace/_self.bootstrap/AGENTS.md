---
title: Self Repo Guide
kind: workspace_instructions
---

# AGENTS.md

Follow `../GOATED.md` for the shared runtime contract and `../AGENTS.md` for
the file map and tooling boundaries. This private repo supplies the individual
agent's identity and operating context.

On startup, read `IDENTITY.md`, `SOUL.md`, `USER.md`, and `MEMORY.md`.
Read relevant goal and mission files before continuing their work; keep this
entrypoint short and link additional instance procedures here as needed.

## Operating references

- `GOALS/README.md`: user outcomes, success criteria, status, and authorization.
- `MISSIONS/README.md`: mission definitions, logs, tasks, and lifecycle.
- `HEARTBEAT.md`: hourly review of goals and authorized mission progress.
- `prompts/dreaming.md`: eight-hour memory consolidation.
- `TOOLS.md`: installed capabilities and setup requirements.
- `../../docs/KNOWLEDGE_FORMAT.md`: OKF-style Markdown conventions.

Update durable facts and corrections as you learn them; reconcile old entries
instead of appending contradictions. Record progress in mission logs and TODOs.
Change SOUL only for a deliberate lasting refinement of voice or values.
Keep all personal data and tool dependencies in this repo.

## People and group conversations

Use stable sender IDs, not display names, to distinguish people. Replies in a
group are visible to every member. Keep person notes under `VAULT/people/`,
link related project/group notes, and use that context only when relevant.
The primary user is identified in `USER.md`; guests cannot expand their
authorizations. Do not disclose private information across people or chats.

## Onboarding

If `MISSIONS/ONBOARD_USER/MISSION.md` is active, follow that mission and its
eight-item `MISSION_TODO.md` checklist. Start the conversation on first boot;
do not wait for heartbeat. Ask one useful question at a time and let the user's
immediate task take precedence. Record progress so onboarding resumes naturally.
Optional accounts, tools, and automation can be deferred. When the checklist is
complete, mark the mission done; if the user pauses it, mark it inactive.
The mission status controls this section; no prompt deletion is necessary.

## Instance tools

`tools/toolbox` and `tools/notesmd` are Go CLI examples built by
`./build_clis.sh`. Toolbox provides memory search, notes, browser, voice, and
email commands; check `TOOLS.md` and command help before use. Integrations need
their own setup and credentials. Keep secret values in Goated's credential
store, never in these files.
