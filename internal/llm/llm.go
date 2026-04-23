// Package llm wraps the optional `claude` CLI pass that rewrites memory
// bodies into richer Obsidian markdown at write time. The package deliberately
// has no dependency on anthropic-sdk-go: the CLI invocation leverages the
// user's Claude Code subscription so there is no per-token billing and no API
// key plumbing in memo.
//
// All exported functions are safe to call with a nil Renderer — callers are
// expected to check Enabled() before invoking Render.
package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ybonda/memo/internal/config"
)

// MaxRenderBytes caps the input size the LLM renderer will accept. Above this
// threshold the `claude -p` subprocess routinely exceeds the 60s default
// timeout (Sonnet at typical output throughput needs ~70-120s for 16-20 KB
// input with preserve-all-content instructions). Rather than burn the full
// timeout and produce no output, we fail fast so the caller keeps the
// deterministic vault body instead. Haiku can handle larger inputs within
// the same window; users running Haiku with bigger memos can bump this
// value in their fork if needed.
const MaxRenderBytes = 20 * 1024

// Renderer rewrites raw memory content into polished Obsidian markdown.
// Implementations may shell out (ClaudeCLI) or use a fake (tests).
type Renderer interface {
	Render(ctx context.Context, raw string) (string, error)
	// Model returns the model identifier this renderer targets (e.g.
	// "haiku", "sonnet"). Used for audit-trail frontmatter so a memo's
	// vault file records which model last rendered its body.
	Model() string
}

// ClaudeCLI invokes `claude -p` (print mode) as a subprocess with the raw
// memory content passed via stdin. The prompt is hardcoded in prompt.go so it
// can iterate in git without users editing config.yaml.
type ClaudeCLI struct {
	cmd     string
	model   string
	timeout time.Duration
}

// NewFromConfig constructs a renderer from the config block. Returns nil when
// LLM export is disabled so callers can use a nil check as "feature off".
func NewFromConfig(cfg config.LLMExportConfig) *ClaudeCLI {
	if !cfg.Enabled {
		return nil
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &ClaudeCLI{
		cmd:     cfg.Command,
		model:   cfg.Model,
		timeout: timeout,
	}
}

// Model returns the configured model name, or "" if unset.
func (c *ClaudeCLI) Model() string { return c.model }

// Render shells out to `claude -p` with the hardcoded prompt and raw content.
// Returns the post-processed rendered markdown or an error describing why the
// pipeline failed. The caller is responsible for choosing a fallback (memo's
// rule: silent fallback to deterministic Format()).
func (c *ClaudeCLI) Render(ctx context.Context, raw string) (string, error) {
	if raw == "" {
		return "", errors.New("empty content")
	}
	if len(raw) > MaxRenderBytes {
		return "", fmt.Errorf("content too large for LLM render: %d bytes exceeds %d limit", len(raw), MaxRenderBytes)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{"-p"}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}
	args = append(args, buildPrompt(raw))

	cmd := exec.CommandContext(ctx, c.cmd, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude invocation failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	out := postprocess(stdout.String())
	if out == "" {
		return "", errors.New("claude returned empty output")
	}
	return out, nil
}

// postprocess strips leading/trailing whitespace and removes a wrapping
// ```markdown ... ``` fence if the model added one despite the prompt's
// instruction. No other normalisation is done — the idea is to keep the
// model's output as a first-class rendered body, not to second-guess it.
func postprocess(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip ```markdown or ``` fences that wrap the entire response.
	for _, prefix := range []string{"```markdown\n", "```md\n", "```\n"} {
		if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, "\n```") {
			s = strings.TrimPrefix(s, prefix)
			s = strings.TrimSuffix(s, "\n```")
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}
