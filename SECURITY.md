# Security Policy

Please do not open public issues for suspected vulnerabilities, credential exposure, or bypasses involving Slack, Telegram, local credential storage, or runtime execution behavior.

Report security issues privately to the maintainers instead. Include:

- A clear description of the issue
- Reproduction steps or a proof of concept
- Expected impact
- Any relevant logs, versions, or environment details

If you are unsure whether something is security-sensitive, report it privately first.

## Scope

Security-sensitive areas in this repo include:

- `workspace/creds/` secret handling
- Slack and Telegram token usage
- Runtime command execution and sandboxing behavior
- Daemon, watchdog, and cron execution paths
- Log redaction and persistence of sensitive data

## Handling

The goal is to acknowledge reports quickly, reproduce them, and ship a fix before public disclosure when practical.
