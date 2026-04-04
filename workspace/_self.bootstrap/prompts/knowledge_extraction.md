---
title: Knowledge Extraction
kind: cron_prompt
---

# Knowledge Extraction

Review recent material in this self repo and extract only durable knowledge.

Before updating `VAULT/`, read `../guides/vault-interlinking.md` for the
current note-linking and vault-organization conventions.

Sources to inspect:
- recent mission logs in `MISSIONS/`
- recent daily notes in `VAULT/daily/`
- recent durable notes already present in `VAULT/`
- any other clearly relevant markdown files in this repo

Write durable facts into:
- `VAULT/people/`
- `VAULT/projects/`
- `VAULT/companies/`
- `VAULT/patterns/`

Rules:
- skip transient chatter
- skip duplicated facts
- prefer updating existing entries over creating near-duplicates
- if you make an important inference, label it as an inference rather than a fact
- if you learn durable facts about the user, make sure `USER.md` stays linked to
  the correct note in `VAULT/people/`
