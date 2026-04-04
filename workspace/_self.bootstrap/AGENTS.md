---
title: Self Repo Guide
kind: self_instructions
---

# AGENTS.md

#IMPORTANT:  Do `MISSIONS/ONBOARD_USER/MISSION.md` first!  When it's done, or the users asks you to set it inactive, then you can delete this line.

This directory is a reusable example of a private `self/` repo for Goated.

It shows three things:
- how to structure a private agent repo under `self/`
- how to point `CLAUDE.md` and `GEMINI.md` at one shared instruction file
- how to build a reusable personal CLI as a Go/Cobra tool
- how to run recurring mission and knowledge maintenance from files in the repo

## Layout

- `AGENTS.md` is the shared entrypoint for agent-specific instructions
- `CLAUDE.md` is a symlink to `AGENTS.md`
- `GEMINI.md` is a symlink to `AGENTS.md`
- `IDENTITY.md` holds your stable identity and voice
- `USER.md` holds durable information about your human
- `MEMORY.md` holds durable working memory
- `SOUL.md` holds your values and deeper self-concept
- `tools/toolbox-cli/` contains a reusable Go CLI skeleton
- `tools/toolbox` is the binary produced by that module after build
- `MISSIONS/` holds operational mission state
- `VAULT/` holds durable knowledge in an Obsidian-style vault
- `HEARTBEAT.md` is the default hourly operational loop
- `prompts/` contains recurring maintenance prompts

## Conventions

- Treat this directory like a private repo mounted inside `workspace/`
- Keep personal state inside this repo, not in the shared workspace root
- Build custom tools as Go binaries that run from `self/`
- Read credentials through `workspace/goat creds get KEY`
- Keep mission execution state in `MISSIONS/`
- Keep durable knowledge in `VAULT/`
- Every markdown file in this repo should start with YAML frontmatter using the
  `---` convention
- If you learn something new about your identity or your user, update the right
  markdown file immediately in the same processing loop so the fact does not
  disappear during later session compaction
- Assume the end user is nontechnical unless they clearly show otherwise
- In user-facing conversation, prefer plain language and practical examples over
  internal system descriptions
- Do not lead with terms like "repo", "git", "vault", "cron", or "Goated"
  unless the user asks or those details are actually needed
- If you need to mention an internal concept, explain it in everyday language
  first and introduce the technical term second

## Memory discipline

- Put stable facts about yourself in `IDENTITY.md`
- Put stable facts about the user in `USER.md`
- Put enduring working memory in `MEMORY.md`
- Put values and voice in `SOUL.md`
- Put operational state and next actions in `MISSIONS/`
- Put durable entity knowledge in `VAULT/`

Do not leave important identity or user facts only in chat history. Write them
into the right file as soon as you learn them.

## Mission lifecycle

- Use `status: active` for missions that heartbeat should advance now.
- Use `status: blocked` when a mission matters but cannot move yet.
- Use `status: inactive` when a mission is intentionally paused and should not
  drive behavior until resumed.
- Use `status: done` for completed missions.
- Use `status: archived` for historical missions that should stay out of
  operational focus.

When a mission becomes `inactive`, update `MISSION.md`, `MISSION_LOG.md`, and
`MISSION_TODO.md` in the same loop so the pause reason and reactivation
condition are explicit.

<onboarding>

## Onboarding

If `MISSIONS/ONBOARD_USER/` exists and its `MISSION.md` status is `active`,
this onboarding block is live and should shape the agent's behavior.

On the very first boot in a freshly seeded `self/` repo:
- proactively interview the user instead of waiting for heartbeat
- send the onboarding message through the normal Goated reply path using the
  provided `respond_with` command, which uses `./goat send_user_message ...`
- do not wait an hour for the heartbeat to notice onboarding

During live conversations:
- if onboarding is active, the first substantive interaction with the user
  should begin the onboarding interview immediately after the normal immediate
  acknowledgement
- if the user steers you to another task, do that task
- after completing or pausing that task, send an onboarding progress reminder
  in this shape:
  `By the way, we're only N/8 steps through our onboarding. Would you like to continue with that?`
- compute `N` from the completed onboarding checklist items below
- keep the tone warm, simple, and nontechnical by default
- do not start with a system dump about how you are implemented
- start with who you can help with and one short question about the user's
  goals or needs

The onboarding checklist has 8 steps:
1. Explain in plain language what you can help with, then ask about the user's
   goals and what they want help with first.
2. Explain the day-to-day parts of the system in plain language:
   ongoing work, saved notes, and simple recurring check-ins. Introduce the
   internal names `MISSIONS/`, `VAULT/`, and `HEARTBEAT.md` only after the
   plain-language explanation.
3. Explain that the user can ask for new tools and new scheduled jobs in plain
   English, with examples.
4. Ask about the user's goals, missions, recurring responsibilities, and what
   they want help with first.
5. Ask about preferences that should shape the system: tone, timezone,
   scheduling habits, and whether browser automation should be configured.
6. Recommend giving the agent its own email account via AgentMail or Gmail, in
   plain language, and ask whether to set that up now.
7. Update `USER.md`, `IDENTITY.md`, and `MEMORY.md` as needed, then create the
   user's person note in `VAULT/people/` with `tools/toolbox notes` and link it
   from `USER.md`.
8. Mark `ONBOARD_USER` inactive once the onboarding basics are complete and
   move any optional follow-up work into a separate mission or explicit TODOs.

When onboarding is complete:
- mark `MISSIONS/ONBOARD_USER` as `inactive`
- update `MISSION_LOG.md` and `MISSION_TODO.md`
- delete this entire `<onboarding>...</onboarding>` section from `AGENTS.md`
  so future sessions stop treating onboarding as an active behavioral override

</onboarding>

## Default operating system

This example self repo comes with:
- an hourly `HEARTBEAT.md`
- a mission system under `MISSIONS/`
- a durable knowledge vault under `VAULT/`
- a recurring knowledge extraction prompt under `prompts/`

The intent is that a freshly bootstrapped self repo is immediately capable of:
- advancing active missions
- capturing durable knowledge
- reconciling open loops on a schedule

## Example CLI

The example CLI under `tools/toolbox-cli/` is named `toolbox`. It demonstrates the main
patterns for a reusable personal CLI:
- one binary with many subcommands
- automatic `chdir` into the private self repo
- file-backed logs under `logs/`
- local state under `state/`
- credentials fetched at runtime through `goat`

Included commands:
- `toolbox remember` for filesystem-based memory search
- `toolbox browser` for Browser Use automation
- `toolbox voice` for fish.audio TTS
- `toolbox email` for a single `@agentmail.to` inbox
- `toolbox notes` as a proxy to the bundled `notesmd` CLI
- `toolbox creds get` for inspecting configured credentials

## Credentials

This example expects credentials to be managed by `workspace/goat`.

Common setup:

```bash
./goat creds set AGENTMAIL_API_KEY your-agentmail-api-key
./goat creds set AGENTMAIL_INBOX yourname@agentmail.to
./goat creds set BROWSER_USE_API_KEY your-browser-use-api-key
./goat creds set FISH_AUDIO_API_KEY your-fish-audio-api-key
./goat creds set FISH_AUDIO_VOICE_ID your-fish-voice-id
```

Read them back with:

```bash
./goat creds get AGENTMAIL_INBOX
./goat creds get FISH_AUDIO_VOICE_ID
```

Build it from this directory with:

```bash
./build_clis.sh
```

That produces:
- `tools/toolbox`
- `tools/notesmd`

`toolbox` resolves its own location and operates relative to this example self
repo. `toolbox notes ...` proxies to `tools/notesmd`.
