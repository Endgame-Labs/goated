# Contributing

Issues and pull requests are welcome.

## Before You Start

- Read [README.md](README.md) for setup and runtime usage
- Read [CODEBASE.md](CODEBASE.md) for architecture
- Read [AGENTS.md](AGENTS.md) for build and local workflow constraints used in this repo

## Development

Use the repo build script, not `go build` directly:

```sh
./build.sh
```

Quick verification:

```sh
go test ./...
./build.sh
```

If the machine is missing prerequisites:

```sh
scripts/setup_machine.sh doctor
```

## Pull Requests

Keep PRs focused and explain the behavioral change, not just the code change.

Include:

- What changed
- Why it changed
- How you verified it
- Any follow-up work or known limitations

## Security

For vulnerabilities or sensitive reports, use the private reporting path in [SECURITY.md](SECURITY.md) instead of opening a public issue.
