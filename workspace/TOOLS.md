# TOOLS.md — Building Agent Tools

This guide is for agents building their own tools on the goated platform.

## Why Go + Cobra

All tools should be built in Go using [Cobra](https://github.com/spf13/cobra) for CLI scaffolding. Here's why:

- **Same stack as `./goat`** — one language, one build system, no runtime dependencies.
- **Single static binary** — no `node_modules/`, no `pip install`, no version conflicts. Copy the binary and it works.
- **Automatic `--help`** — Cobra generates usage docs, flag parsing, and subcommand trees for free. Every tool is self-documenting.
- **Minimal dependencies** — the Go stdlib covers HTTP, JSON, file I/O, and concurrency. You rarely need external packages beyond Cobra.
- **Fast startup** — Go binaries launch in milliseconds. No interpreter warmup, no JIT.

## Project structure

Create a Go module in your workspace. One module can produce one binary with many subcommands:

```
tools/go/
  go.mod              # module name, e.g. "agent-tools"
  go.sum
  cmd/mytool/
    main.go           # entry point, registers subcommands
    foo.go            # `mytool foo` subcommand
    bar.go            # `mytool bar` subcommand
  internal/
    creds/creds.go    # credential helper (see below)
    httputil/http.go  # shared HTTP helpers (optional)
```

Initialize with:
```bash
cd tools/go
go mod init agent-tools
go get github.com/spf13/cobra
```

## Minimal example

A complete tool with one subcommand:

```go
// cmd/mytool/main.go
package main

import (
    "fmt"
    "os"
    "github.com/spf13/cobra"
)

func main() {
    root := &cobra.Command{Use: "mytool", Short: "My agent's tools"}
    root.AddCommand(helloCmd())
    if err := root.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func helloCmd() *cobra.Command {
    var name string
    cmd := &cobra.Command{
        Use:   "hello",
        Short: "Say hello",
        RunE: func(cmd *cobra.Command, args []string) error {
            fmt.Printf("Hello, %s!\n", name)
            return nil
        },
    }
    cmd.Flags().StringVarP(&name, "name", "n", "world", "Who to greet")
    return cmd
}
```

Build and run:
```bash
cd tools/go
go build -o ../mytool ./cmd/mytool/
../mytool hello --name agent
# Hello, agent!
../mytool --help
# Shows all subcommands automatically
```

## Reading credentials

Credentials are managed by `./goat creds` (see GOATED_CLI_README.md). Tools should shell out to read them at runtime — never hardcode secrets.

```go
// internal/creds/creds.go
package creds

import (
    "fmt"
    "os/exec"
    "strings"
)

const goatBin = "/path/to/workspace/goat"  // set to your workspace's goat binary

func Get(key string) (string, error) {
    out, err := exec.Command(goatBin, "creds", "get", key).Output()
    if err != nil {
        return "", fmt.Errorf("creds get %s: %w", key, err)
    }
    return strings.TrimSpace(string(out)), nil
}
```

Usage in a subcommand:
```go
token, err := creds.Get("MY_API_KEY")
if err != nil {
    return fmt.Errorf("need MY_API_KEY: %w", err)
}
```

Set credentials via the CLI before first use:
```bash
./goat creds set MY_API_KEY sk-xxx
```

## Making HTTP requests

Go's stdlib `net/http` + `encoding/json` handles most API work. A minimal helper:

```go
func postJSON(url string, body any, token string, result any) error {
    data, _ := json.Marshal(body)
    req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
    req.Header.Set("Content-Type", "application/json")
    if token != "" {
        req.Header.Set("Authorization", "Bearer "+token)
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    return json.NewDecoder(resp.Body).Decode(result)
}
```

No need for `axios`, `node-fetch`, or `requests`. The stdlib is enough.

## Adding subcommands

Each subcommand lives in its own file for clarity. The pattern:

1. Define a function that returns `*cobra.Command`
2. Register it in `main.go` with `root.AddCommand(myCmd())`
3. Use `RunE` (not `Run`) so errors propagate cleanly

```go
// cmd/mytool/weather.go
func weatherCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "weather [city]",
        Short: "Get weather for a city",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            city := args[0]
            // ... fetch weather ...
            fmt.Printf("Weather in %s: sunny\n", city)
            return nil
        },
    }
}
```

Nested subcommands work too:
```go
func platformCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "platform", Short: "Platform operations"}
    cmd.AddCommand(platformPostCmd())
    cmd.AddCommand(platformReadCmd())
    return cmd
}
// mytool platform post "hello"
// mytool platform read --count 10
```

## Working directory: always `self/`

Your tool binary lives in `self/tools/`. Any files it writes (archives, vault notes, state, etc.) MUST land inside `self/`, never in the workspace root. The workspace root is the shared goated repo — personal files written there will pollute it.

**Every tool MUST set its working directory to `self/` at startup** using `os.Executable()` to find itself. This is absolute and works regardless of where the binary is invoked from:

```go
// In your root command's PersistentPreRunE (runs before every subcommand):
root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
    exe, err := os.Executable()
    if err != nil {
        return fmt.Errorf("resolve executable path: %w", err)
    }
    exe, err = filepath.EvalSymlinks(exe)
    if err != nil {
        return fmt.Errorf("resolve symlinks: %w", err)
    }
    // Binary is at self/tools/<name>, so two levels up = self/
    selfDir := filepath.Dir(filepath.Dir(exe))
    if err := os.Chdir(selfDir); err != nil {
        return fmt.Errorf("chdir to self: %w", err)
    }
    return nil
}
```

After this runs, all relative paths (e.g., `vault/`, `posts/`, `state/`) resolve inside `self/`. No subcommand needs to think about it.

**Required imports** for this pattern: `os`, `path/filepath`.

## Guidelines

- **One binary, many subcommands.** Don't create separate binaries for every task. Group related operations under subcommands.
- **Use `RunE`, not `Run`.** Return errors instead of calling `os.Exit()` or `log.Fatal()` inside commands. Cobra handles the error display.
- **Flags over env vars.** Use Cobra flags with sensible defaults. Flags are self-documenting via `--help`.
- **No global state.** Each command function should be self-contained. Read creds and config inside `RunE`, not at package init.
- **Print structured output.** When tools produce data for other tools, use JSON. When producing output for humans/agents reading the terminal, use readable text.
- **Never write to the workspace root.** All file output (archives, vault, state, posts) goes in `self/`. The `PersistentPreRunE` pattern above handles this automatically.
- **Archive to markdown.** If a tool creates or consumes content (posts, messages, etc.), save a copy to a local markdown file for memory. Use a `YYYY-MM-DD` directory structure.
- **Keep dependencies minimal.** Cobra is the only required external dependency. Resist adding more unless there's a strong reason. The Go stdlib is very capable.
