# toolbox — personal CLI example

This module is a reusable template for building a personal Goated CLI.

It keeps the useful structure from the original toolchain this template was based on:
- Cobra-based single binary
- dynamic discovery of the enclosing `self/` directory
- logs written to `logs/<tool>/`
- state written inside the self repo
- credentials fetched through `workspace/goat`

It removes identity-specific details:
- no hardcoded identity
- no platform-specific accounts
- no absolute paths baked into source

## Build

```bash
cd tools/toolbox-cli
go build -o ../toolbox ./cmd/toolbox
```

## Commands

- `toolbox creds get AGENTMAIL_INBOX`
- `toolbox remember "person or topic"`
- `toolbox browser run --task "open example.com and summarize it" --wait`
- `toolbox voice say --text "hello world" --no-catbox`
- `toolbox email check`
- `toolbox email send --to friend@example.com --subject "Hello" --body "Hi there"`
- `toolbox notes search "query"`
- `toolbox notes search-content -v vault "query"`

## Adding tools

The core pattern is:
- one binary: `toolbox`
- one file per top-level command group under `cmd/toolbox/`
- optional nested subcommands returned by helper functions
- shared helpers under `internal/`

### Add a top-level command

1. Add a new file under `cmd/toolbox/`, for example `calendar.go`.
2. Define a function that returns `*cobra.Command`.
3. Register it in `cmd/toolbox/main.go` with `rootCmd.AddCommand(calendarCmd())`.

Minimal example:

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func calendarCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "calendar",
		Short: "Calendar operations",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("calendar root")
			return nil
		},
	}
}
```

Register it in `cmd/toolbox/main.go`:

```go
rootCmd.AddCommand(calendarCmd())
```

### Add subcommands under a tool

For anything non-trivial, make the top-level command a group and attach
subcommands.

Pattern:

```go
func calendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Calendar operations",
	}
	cmd.AddCommand(calendarListCmd())
	cmd.AddCommand(calendarCreateCmd())
	return cmd
}

func calendarListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List upcoming events",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("listing events")
			return nil
		},
	}
}
```

That gives you commands like:
- `toolbox calendar`
- `toolbox calendar list`
- `toolbox calendar create`

### Real examples in this template

Top-level commands:
- `cmd/toolbox/browser.go` adds `toolbox browser`
- `cmd/toolbox/email.go` adds `toolbox email`
- `cmd/toolbox/voice.go` adds `toolbox voice`
- `cmd/toolbox/remember.go` adds `toolbox remember`
- `cmd/toolbox/notes.go` adds `toolbox notes`

Nested subcommands:
- `browserCmd()` registers `run`, `status`, `result`, `tasks`, `profiles`, and others
- `voiceCmd()` registers `say`
- `emailCmd()` registers `check` and `send`
- `credsCmd()` registers `get`

### Proxying another CLI through `toolbox`

`toolbox notes` is an example of a proxy command rather than a native Cobra
subtree. It forwards all remaining args to the bundled `tools/notesmd` binary.

The key pieces are:
- `DisableFlagParsing: true` so Cobra does not consume flags meant for `notesmd`
- `exec.Command(...)` to run the target binary
- passing through `stdin`, `stdout`, and `stderr`

That means you can expose an external CLI under the main `toolbox` entrypoint
without rewriting that CLI in Cobra.

## Build both CLIs

From `_self.bootstrap/`:

```bash
./build_clis.sh
```

That builds:
- `tools/toolbox` from `tools/toolbox-cli`
- `tools/notesmd` from `tools/notesmd-cli`

The bundled `notesmd` source lives in `tools/notesmd-cli/`, and `toolbox notes
...` forwards arguments directly to the built `tools/notesmd` binary.

## `goat creds` setup

`toolbox` reads secrets and defaults through `workspace/goat`, not environment
variables.

Required keys by feature:

```bash
./goat creds set AGENTMAIL_API_KEY your-agentmail-api-key
./goat creds set AGENTMAIL_INBOX yourname@agentmail.to
./goat creds set BROWSER_USE_API_KEY your-browser-use-api-key
./goat creds set FISH_AUDIO_API_KEY your-fish-audio-api-key
./goat creds set FISH_AUDIO_VOICE_ID your-fish-voice-id
```

Useful checks:

```bash
./goat creds get AGENTMAIL_INBOX
./goat creds get BROWSER_USE_API_KEY
./goat creds get FISH_AUDIO_VOICE_ID
```

If `FISH_AUDIO_VOICE_ID` is not set, pass `--voice` to `toolbox voice say`.

## Adapting this template

1. Rename the module and binary.
2. Add platform subcommand groups in `cmd/toolbox/`.
3. Reuse `internal/creds` for runtime credential lookup.
4. Reuse `internal/httputil` for JSON APIs.
5. Use `toolbox notes ...` when you want to expose `notesmd` through your main CLI.
6. Keep all writes inside the self repo.
