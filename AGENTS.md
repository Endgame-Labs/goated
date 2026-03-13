# AGENTS.md — Developer guide for AI agents

For codebase architecture, project structure, and how the system works, see [CODEBASE.md](CODEBASE.md).
For agent runtime instructions (responding to users, memory, identity), see [workspace/CLAUDE.md](workspace/CLAUDE.md).

## Building

If this machine is missing Go or other prerequisites, run:

```bash
scripts/setup_machine.sh doctor
```

**Always use `build.sh`** — never `go build` directly.

```bash
./build.sh              # builds all three binaries
```

Three binaries are produced:
- `./goated` — control CLI
- `./goated_daemon` — gateway daemon
- `./workspace/goat` — agent CLI (used by Claude inside workspace)

## Running the daemon

```bash
./build_all_and_run_daemon.sh   # builds everything, then starts daemon
```

Or if already built:
```bash
./goated_daemon                 # self-daemonizes, logs to logs/goated_daemon.log
```

The daemon self-backgrounds. No `nohup &` needed.

## Restarting the daemon

```bash
pkill -f goated_daemon
./build_all_and_run_daemon.sh
```

## Verifying changes compile

```bash
go build ./...          # quick compile check
./build.sh              # full build
```

No automated tests yet. Test manually by sending messages through the gateway.

## Machine setup

Goated expects:
- Go matching `go.mod`
- `tmux`
- one runtime CLI on `PATH`: `claude` or `codex`

Useful commands:

```bash
scripts/setup_machine.sh doctor
scripts/setup_machine.sh install-system
scripts/setup_machine.sh install-go
```
