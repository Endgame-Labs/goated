---
type: Guide
title: Vault
kind: knowledge_index
---

# VAULT

This is the default Obsidian-style vault for durable knowledge.

Use `VAULT/` for things that should survive beyond the current task or session.
Use `MISSIONS/` for in-flight execution state.

## Suggested areas

- `people/` — durable facts about people
- `projects/` — durable facts about projects
- `companies/` — durable facts about companies
- `patterns/` — reusable heuristics, lessons, and patterns
- `daily/` — daily notes and reconciliations

## How to use it

- Use [OKF-inspired Markdown](../../../docs/KNOWLEDGE_FORMAT.md) with YAML
  frontmatter, type/title, source references, and portable relative links.
- Prefer one entity per file.
- Keep facts crisp and durable.
- Link related entities where helpful.
- Reconcile changed facts in the canonical note; label uncertainty and retain
  original evidence. Dreaming reports are an audit trail, not independent sources.
- For the human user, keep a single person note under `people/` and link it
  from `USER.md`.

## NotesMD examples

```bash
tools/notesmd list -v VAULT
tools/notesmd search-content -v VAULT "search term"
tools/notesmd print -v VAULT people/example-person.md
```
