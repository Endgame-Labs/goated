# Building a Living Knowledge Graph with Obsidian-Style Wikilinks

A practical guide for turning a flat collection of markdown files into an interlinked knowledge graph that AI agents can navigate, maintain, and grow.

---

## The Problem

You have a vault full of notes — people, papers, patterns, projects. Each file is accurate on its own. But they're islands. An agent reading about a person has no way to discover the paper that person wrote, the company they work at, or the pattern their work exemplifies. The knowledge is there; the connections aren't.

Obsidian solves this for humans with a visual graph. For agents, we need something they can parse: **wikilinks as structured edges** plus **toolbox commands that audit the graph**.

---

## Vault Structure

Organize notes by type, one directory per type:

```
vault/
├── people/          # Humans, agents, contacts
├── companies/       # Organizations
├── projects/        # Software, initiatives, ventures
├── patterns/        # Ideas, frameworks, recurring themes
├── papers/          # Academic papers
├── reading/         # Books, thinkers
│   └── blogs/       # Articles, blog posts
├── insights/        # Dated reflections
├── experiments/     # Experiment logs
├── press/           # News and coverage
└── infrastructure/  # Services and tools
```

Every file gets YAML frontmatter. The `type` field determines which directory it lives in.

---

## Types and Frontmatter

| Type | Directory | Required frontmatter |
|------|-----------|---------------------|
| `person` | `people/` | `type`, `aliases`, `tags`, `created`, `updated` |
| `company` | `companies/` | `type`, `aliases`, `tags`, `created`, `updated` |
| `project` | `projects/` | `type`, `title`, `created`, `updated` |
| `idea` | `patterns/` | `type`, `title`, `created`, `updated`, `source` |
| `paper` | `papers/` | `type`, `title`, `authors`, `source` (URL), `created`, `year` |
| `book` | `reading/` | `type: book`, `title`, `author`, `source` (URL), `created` |
| `article` | `reading/blogs/` | `type: article`, `title`, `author`, `source` (URL), `created` |

**`source` is mandatory for papers, books, and articles.** It must be a URL pointing to the original content — arXiv, DOI, publisher page, blog post. Without it, agents can't verify or revisit the material.

### Frontmatter Examples

```yaml
# Person (people/ada-lovelace.md)
---
type: person
aliases: [Ada, Countess of Lovelace]
tags: [mathematician, pioneer]
created: 2026-03-21
updated: 2026-03-21
---

# Paper (papers/attention-is-all-you-need.md)
---
type: paper
title: "Attention Is All You Need"
authors: ["Vaswani", "Shazeer", "Parmar", "Uszkoreit", "Jones", "Gomez", "Kaiser", "Polosukhin"]
source: https://arxiv.org/abs/1706.03762
year: 2017
created: 2026-03-21
---

# Book (reading/cosmos.md)
---
type: book
title: "Cosmos"
author: Carl Sagan
source: https://en.wikipedia.org/wiki/Cosmos_(Sagan_book)
created: 2026-03-21
---

# Article (reading/blogs/2026-03-20-turbo-pascal.md)
---
type: article
title: "Turbo Pascal 3.02A, deconstructed"
author: Simon Willison
source: https://simonwillison.net/2026/Mar/20/turbo-pascal/
created: 2026-03-20
---

# Idea (patterns/constraint-as-generator.md)
---
type: idea
title: "Constraint as Generator"
source: "Fable's divine CLI experiment, March 2026"
created: 2026-03-21
updated: 2026-03-21
---
```

---

## Wikilink Format

Always use the full path with a display alias:

```
[[path/filename|Display Name]]
```

Examples:
```
[[people/ada-lovelace|Ada Lovelace]]
[[companies/anthropic|Anthropic]]
[[papers/attention-is-all-you-need|Attention Is All You Need]]
[[patterns/constraint-as-generator|constraint as generator]]
[[reading/cosmos|Cosmos]]
```

The path prefix removes ambiguity (there could be a person and a project with the same name). The alias keeps prose readable.

---

## Linking Rules

These nine rules turn a directory of files into a graph:

1. **Every new vault file MUST have at least one outbound wikilink** to a related entity in a different directory.

2. **When mentioning a known entity by name in prose, use a wikilink** (first mention per section only).

3. **Every file should have a `## Related` or `## See Also` section** at the bottom with explicit cross-category links.

4. **People files must link to companies/projects they're associated with** and other people they interact with. Aim for 3–5 cross-category links per person entry.

5. **Pattern files must link to people who originated them** and papers that support them.

6. **Reading notes must link to related patterns and papers.**

7. **Frontmatter tags complement but do not replace wikilinks.** Tags are for filtering; wikilinks are for specific connections.

8. **Run `toolbox vault check-links` before committing** to catch broken links and missing opportunities.

9. **Verify link targets exist before creating them.** Run `ls vault/path/filename.md` to confirm. Never create a wikilink to a file that doesn't exist — phantom links clutter the graph.

### Anti-patterns

- Don't over-link — first mention per section is enough.
- Don't link inside code blocks or frontmatter.
- Don't create phantom links to files that don't exist.
- Tags alone are not sufficient interlinking.

---

## The Check-Links Toolbox Command

The core of the system is a single CLI command that audits the entire graph:

```bash
toolbox vault check-links
```

It reports three things:

### 1. Broken Links
Wikilinks pointing to files that don't exist:
```
papers/some-paper.md:17 → [[people/nonexistent]] (not found)
```

### 2. Orphan Files
Vault files that no other file links to — isolated islands:
```
Orphan: people/forgotten-person.md (0 inbound links)
```

### 3. Missing Link Suggestions
Files that mention known entity names in prose but don't wikilink them:
```
people/alice.md mentions "Anthropic" → suggest [[companies/anthropic|Anthropic]]
```

The entity index is built from three sources: filenames, frontmatter `aliases`, and H1 headings. This catches most references.

### Flags

```bash
toolbox vault check-links --broken-only       # just broken links
toolbox vault check-links --orphans-only      # just orphans
toolbox vault check-links --suggestions-only  # just suggestions
toolbox vault check-links --fix               # auto-fix suggestions (adds wikilinks)
toolbox vault check-links --section people/   # scope to one directory
```

---

## Building check-links

The command needs to:

1. **Scan all `.md` files** in the vault.
2. **Extract all wikilinks** using regex: `\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`
3. **Build an entity index** from:
   - Filenames (strip `.md`, use last path segment as name)
   - YAML frontmatter `aliases` arrays
   - First H1 heading (`# Name`)
4. **Check each link target** — does `vault/{path}.md` exist?
5. **Count inbound links** per file to find orphans.
6. **Scan prose for entity names** that aren't already wikilinked.

For `--fix` mode: when a suggestion match is found, replace the plain-text mention with `[[path/filename|Display Name]]` (first occurrence per section only).

The implementation is ~800 lines of Go, but you could build it in Python or any language. The key is the entity index — without aliases and H1 matching, you'll miss most suggestions.

---

## Retrofitting an Existing Vault

If you already have hundreds of unlinked files, here's the approach:

### Phase 1: Audit
```bash
toolbox vault check-links
```

This gives you the full picture: how many broken links, orphans, and missing suggestions.

### Phase 2: Prioritize People
People files are the highest-connectivity nodes. Start there:
```bash
toolbox vault check-links --suggestions-only --section people/
```

Each person should link to their company, projects, and related people. Retrofit these first for maximum graph density.

### Phase 3: Fix Broken Links
Either create the missing target files or remove/fix the broken links:
```bash
toolbox vault check-links --broken-only
```

### Phase 4: Connect Patterns and Papers
Pattern files should link to their originators. Paper files should link to related patterns and the researchers.

### Phase 5: Auto-fix Where Safe
```bash
toolbox vault check-links --fix
```

Review the diff before committing. Auto-fix handles the obvious cases; edge cases need human judgment.

### Parallelization
If you have multiple agents, split the work:
- Agent A: people files A–M
- Agent B: people files N–Z
- Agent C: papers and patterns
- Agent D: reading notes

Each agent reads the convention (this guide or the AGENTS.md section), does its section, runs check-links to verify.

---

## Pre-commit Hook

Install a git hook to prevent broken links from being committed:

```bash
#!/bin/bash
# .git/hooks/pre-commit
TOOLBOX="$(git rev-parse --show-toplevel)/tools/toolbox"

if [ ! -x "$TOOLBOX" ]; then
  echo "⚠️  toolbox binary not found — skipping vault link check"
  exit 0
fi

OUTPUT=$("$TOOLBOX" vault check-links --broken-only 2>&1)
COUNT=$(echo "$OUTPUT" | head -1 | grep -oP '\d+' | head -1)

if [ -n "$COUNT" ] && [ "$COUNT" -gt 0 ]; then
  echo "❌ Vault has $COUNT broken wikilinks. Fix before committing."
  echo "$OUTPUT" | head -20
  exit 1
fi

exit 0
```

This catches the most dangerous anti-pattern: phantom links that reference files nobody ever created.

---

## Convention Discoverability

For multi-agent systems, the convention must be findable without being told about it. The chain:

1. **Entry point** (`CLAUDE.md` or equivalent) points to the agent instructions file.
2. **Agent instructions** (`AGENTS.md`) contain the full vault interlinking convention.
3. **The convention itself** references `toolbox vault check-links` as the verification step.

An agent starting from scratch reads the entry point, follows the chain, and finds the rules. No briefing needed.

### Testing Discoverability

Spawn a fresh agent with a minimal prompt like:
> "Add a new person entry to the vault for Jane Doe, AI researcher at Anthropic."

If the agent:
- Finds and follows the wikilink convention
- Creates proper frontmatter with the right type fields
- Adds cross-category wikilinks (to the company, related people)
- Verifies link targets exist before linking
- Runs check-links before committing

...then your convention is discoverable. If it doesn't, trace where the chain broke and fix the docs.

---

## Semantic Search Layer

Wikilinks give you explicit, curated connections. For implicit connections — "what vault entries are conceptually related to X?" — add a vector search layer:

1. **Index vault files** to a vector database (e.g., Turbopuffer) using embeddings.
2. **Run indexing on a schedule** (hourly cron) so new files are searchable quickly.
3. **Combine both**: wikilinks for known relationships, vector search for discovery.

This is how you find the connection between a paper on attention mechanisms and a pattern about constraint-as-generator that nobody thought to link explicitly.

---

## Summary

The system has three layers:

1. **Convention** — wikilink format, frontmatter types, linking rules, documented in agent instructions.
2. **Tooling** — `check-links` command that audits broken links, orphans, and suggestions; pre-commit hook that blocks broken links.
3. **Discovery** — vector search for implicit connections; check-links suggestions for explicit ones agents missed.

Start with the convention. Build the tooling. Let agents grow the graph. The vault becomes a living knowledge base — not a filing cabinet, but a web of meaning that any agent can navigate.
