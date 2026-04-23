package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybonda/memo/internal/config"
)

func TestPostprocessStripsMarkdownFence(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", "hello world", "hello world"},
		{"leading trailing whitespace", "  \n\nhello\n\n  ", "hello"},
		{"md fence", "```md\nhello\n```", "hello"},
		{"markdown fence", "```markdown\n# heading\n\nbody\n```", "# heading\n\nbody"},
		{"bare fence that actually wraps everything", "```\nsome body\n```", "some body"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := postprocess(c.in); got != c.want {
				t.Errorf("postprocess(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestBuildPromptIncludesContent(t *testing.T) {
	raw := "OPS-43243: incident details"
	out := buildPrompt(raw)
	if !strings.Contains(out, raw) {
		t.Errorf("prompt missing raw content:\n%s", out)
	}
	if !strings.Contains(out, "BEGIN MEMORY CONTENT") ||
		!strings.Contains(out, "END MEMORY CONTENT") {
		t.Errorf("prompt missing delimiters:\n%s", out)
	}
}

func TestNewFromConfigDisabled(t *testing.T) {
	r := NewFromConfig(config.LLMExportConfig{Enabled: false})
	if r != nil {
		t.Errorf("expected nil renderer when Enabled=false, got %+v", r)
	}
}

// TestRenderWithFakeBinary shells out to a throwaway shell script that
// echoes a canned response, verifying the subprocess plumbing (args, stdin,
// stdout capture) without depending on `claude` being installed.
func TestRenderWithFakeBinary(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	binPath := filepath.Join(dir, "fake-claude")
	script := `#!/bin/sh
# Ignore all args; emit a predictable, already-post-processable response.
echo "## Rendered"
echo ""
echo "Body line one."
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewFromConfig(config.LLMExportConfig{
		Enabled:        true,
		Command:        binPath,
		TimeoutSeconds: 5,
	})
	out, err := r.Render(context.Background(), "raw memory content")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## Rendered") {
		t.Errorf("expected canned response in output, got:\n%s", out)
	}
}

func TestRenderFailsFast(t *testing.T) {
	r := NewFromConfig(config.LLMExportConfig{
		Enabled:        true,
		Command:        "/nonexistent/definitely/not/claude",
		TimeoutSeconds: 2,
	})
	_, err := r.Render(context.Background(), "content")
	if err == nil {
		t.Errorf("expected error when binary is missing")
	}
}

func TestRenderEmptyInput(t *testing.T) {
	r := NewFromConfig(config.LLMExportConfig{
		Enabled:        true,
		Command:        "true",
		TimeoutSeconds: 2,
	})
	if _, err := r.Render(context.Background(), ""); err == nil {
		t.Errorf("expected error for empty input")
	}
}

func TestRenderRejectsOversizedInput(t *testing.T) {
	r := NewFromConfig(config.LLMExportConfig{
		Enabled:        true,
		Command:        "claude",
		TimeoutSeconds: 2,
	})
	oversize := strings.Repeat("a", MaxRenderBytes+1)
	_, err := r.Render(context.Background(), oversize)
	if err == nil {
		t.Fatal("expected error for oversized input")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention size; got: %v", err)
	}
}

func TestRenderTimeoutHonored(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	binPath := filepath.Join(dir, "slow-claude")
	// `exec` replaces the shell process with sleep so SIGKILL on context
	// timeout actually terminates it (otherwise the shell is killed but its
	// sleep child keeps running until exit 5s).
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &ClaudeCLI{cmd: binPath, timeout: 250 * time.Millisecond}
	start := time.Now()
	_, err := r.Render(context.Background(), "content")
	elapsed := time.Since(start)
	if err == nil {
		t.Errorf("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout not enforced; elapsed=%v", elapsed)
	}
}
