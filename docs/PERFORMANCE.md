# Performance Comparison: Goated vs OpenClaw

Most agent frameworks own the context window — they inject bootstrap files, manage session history, and accumulate state in-process until memory explodes. Goated doesn't touch the context window at all. It's a ~20 MB daemon that pastes message envelopes into tmux and lets Claude Code handle its own context compaction, memory, and token budgeting. The result: no token bloat, no session file growth, no multi-GB memory leaks. Just a thin orchestrator that stays out of the way.

The tables below compare Goated against OpenClaw (a popular self-hosted agent framework) across token usage, storage, memory, and architecture.

## Token Usage

| | OpenClaw | Goated |
|---|---|---|
| **System prompt overhead** | 3K-5K tokens/message | Delegated to Claude Code (not managed) |
| **Context injection** | 3K-14K tokens/message (bootstrap files force-injected) | ~200 chars prompt envelope; Claude Code loads its own files on demand |
| **Session context bloat** | 200K+ tokens of old history reprocessed per turn; 111 KB / 28K tokens appended per request | Tmux scrollback is the session -- context is whatever Claude Code manages internally |
| **Configurable caps** | `bootstrapMaxChars` (20K), `bootstrapTotalMaxChars` (150K) | None needed -- goated doesn't inject context, Claude Code handles its own window |

**Key difference**: OpenClaw owns the context pipeline and injects bootstrap files, tool outputs, and session history into every request. Goated doesn't touch the context window at all -- it pastes a small envelope into tmux and lets Claude Code manage its own context compaction, memory, and token budgeting. The trade-off is less control over token optimization, but the resource profile is orders of magnitude lighter.

## File Size / Storage

| | OpenClaw | Goated |
|---|---|---|
| **Session files** | `.jsonl` files that grow unbounded as tool outputs accumulate | No session files -- tmux scrollback is ephemeral |
| **Database** | Not applicable (state in session files) | bbolt `goated.db`, ~10-20 MB after sustained use |
| **Logs** | Embedded in session state | Separate append-only files (`runs.jsonl`, per-job logs) |
| **Agent context files** | Force-injected up to 150K chars | Committed to `workspace/`, loaded by Claude Code on its own terms |

**Key difference**: OpenClaw's `.jsonl` session files are the primary source of bloat -- large tool outputs get stored permanently and dragged forward. Goated has no equivalent. Logs are append-only files on disk, not in-memory state.

## Memory / Process Overhead

| | OpenClaw | Goated |
|---|---|---|
| **Daemon RSS** | 1.9 GB after 13h; 28 GB by day 3 (self-hosted) | **~15-20 MB** steady state |
| **Memory growth** | Linear/unbounded -- session state never GC'd | Flat -- open-per-op DB pattern, no held state |
| **CPU** | 69.9% after 13h (gateway process) | Negligible (poll tmux every 2s, run cron every 1m) |
| **Recovery** | Manual restart or OOM-kill | Auto-restart on health check failure; graceful drain |

**Key difference**: OpenClaw accumulates session state in RAM and never prunes it, leading to GB-scale memory growth. Goated's daemon holds almost nothing in memory -- the database is opened and closed per operation, subagents are separate processes, and the only persistent state is a tmux session managed by the OS.

## Claude Code Process (the elephant in the room)

Goated's daemon is ~15-20 MB, but the `goat_main` tmux session runs a persistent Claude Code process that is subject to its own memory issues.

### Claude Code Memory Profile

| Scenario | Observed RSS |
|---|---|
| Healthy idle session | ~200-500 MB |
| Typical active session | ~800 MB - 1 GB |
| Long session (14h+) | 23 GB+ (with 143% CPU) |
| `--continue` with large history | 28 GB |
| Subagent spawning | 100+ GB reported by multiple users |
| v2.1.32 regression | ~11 GB on launch (within 20 seconds) |
| Worst case (OOM-kill) | 93-129 GB before system freeze |

Anthropic has been actively patching these -- recent changelogs mention fixes for memory growth from large shell output, session resumption, and stream-json output. But the pattern of memory leaks in long-running sessions keeps recurring across versions.

The auto-restart health checks in `tmux_bridge.go` are a critical safety valve here -- if Claude Code bloats or crashes, goated detects it and restarts the session.

**Real steady-state profile**: ~15-20 MB (goated) + 500 MB to multi-GB (Claude Code, version-dependent).

## Architecture Philosophy

| | OpenClaw | Goated |
|---|---|---|
| **Context management** | Owns the full context pipeline | Delegates entirely to Claude Code |
| **Session state** | In-process, in-memory | Out-of-process (tmux + bbolt) |
| **Scaling model** | Single process accumulates everything | Thin orchestrator + isolated subagent processes |
| **DB pattern** | N/A (file-based) | Open-per-op (no lock contention, no held connections) |
| **Failure recovery** | OOM-kill then restart | Health checks then auto-restart with graceful drain |

## Bottom Line

Goated sidesteps the entire class of problems that plague OpenClaw's steady state by **not owning the context window**. OpenClaw's token bloat, session file growth, and memory leaks all stem from it managing the full LLM context pipeline in-process. Goated is a ~15-20 MB message router that pastes prompts into tmux and lets Claude Code handle its own context compaction, memory, and token budgeting.

## Sources

- [OpenClaw Token Use and Costs](https://docs.openclaw.ai/reference/token-use)
- [OpenClaw: Burning through tokens - Discussion #1949](https://github.com/openclaw/openclaw/discussions/1949)
- [Why AI Agents like OpenClaw Burn Through Tokens - Milvus Blog](https://milvus.io/blog/why-ai-agents-like-openclaw-burn-through-tokens-and-how-to-cut-costs.md)
- [OpenClaw Memory Runaway - moltworker #253](https://github.com/cloudflare/moltworker/issues/253)
- [OpenClaw Gateway Memory - openclaw #13758](https://github.com/openclaw/openclaw/issues/13758)
- [OpenClaw Production Guide - SitePoint](https://www.sitepoint.com/openclaw-production-lessons-4-weeks-self-hosted-ai/)
- [Claude Code Memory Leak - 120+ GB - Issue #4953](https://github.com/anthropics/claude-code/issues/4953)
- [Claude Code 129GB Leak - Issue #11315](https://github.com/anthropics/claude-code/issues/11315)
- [Claude Code 14h Memory Leak - Issue #11377](https://github.com/anthropics/claude-code/issues/11377)
- [Claude Code --continue 28GB - Issue #10505](https://github.com/anthropics/claude-code/issues/10505)
- [Claude Code v2.1.32 11GB on Launch - Issue #23442](https://github.com/anthropics/claude-code/issues/23442)
