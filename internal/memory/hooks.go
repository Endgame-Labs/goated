package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CommandHook receives a JSON array on stdin and must emit a JSON array on
// stdout. Context is metadata in GOAT_MEMORY_HOOK_CONTEXT, not part of the
// array contract. Hooks run in configured order and must not mutate inputs.
type CommandHook struct {
	Args     []string
	Dir      string
	Timeout  time.Duration
	MaxBytes int64
}

func (h CommandHook) Apply(ctx context.Context, chunks []Chunk, state HookContext) ([]Chunk, error) {
	if len(h.Args) == 0 {
		return nil, errors.New("hook command is empty")
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	input := mustJSON(chunks)
	metadata, _ := json.Marshal(state)
	cmd := exec.CommandContext(ctx, h.Args[0], h.Args[1:]...)
	cmd.Dir = h.Dir
	cmd.Env = append(os.Environ(), "GOAT_MEMORY_HOOK_CONTEXT="+string(metadata))
	cmd.Stdin = bytes.NewReader(input)
	var out, stderr bytes.Buffer
	max := h.MaxBytes
	if max <= 0 {
		max = 1 << 20
	}
	cmd.Stdout = &limitedWriter{w: &out, n: max}
	cmd.Stderr = &limitedWriter{w: &stderr, n: 2048}
	if err := runCommand(cmd); err != nil {
		return nil, fmt.Errorf("memory hook: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return ParseResults(out.Bytes())
}
