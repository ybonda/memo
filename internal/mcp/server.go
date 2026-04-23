package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ybonda/memo/internal/capture"
	"github.com/ybonda/memo/internal/config"
	"github.com/ybonda/memo/internal/store"
	"github.com/ybonda/memo/internal/version"
)

func logErr(tool string, err error) {
	fmt.Fprintf(os.Stderr, "[memo-serve] tool %s error: %v\n", tool, err)
}

type Handler struct {
	store  *store.MemoryStore
	config *config.Config
}

// Serve starts the MCP server. The stdout parameter is the clean writer
// for JSON-RPC messages — os.Stdout must be redirected to stderr before
// calling this so that library noise (hugot/GoMLX) cannot corrupt the
// JSON-RPC stream.
func Serve(s *store.MemoryStore, cfg *config.Config, stdout io.Writer) error {
	h := &Handler{store: s, config: cfg}

	srv := server.NewMCPServer("memo", version.Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions("IMPORTANT: When calling memo tools via mcp-cli, you MUST use the --json flag or output will be invisible. Example: mcp-cli call --json memo/memo_search '{\"query\": \"...\", \"limit\": 5}'"),
	)

	typeNames := typeEnum(cfg)

	// memo_remember
	srv.AddTool(mcp.NewTool("memo_remember",
		mcp.WithDescription("Store a new memory with semantic dedup detection"),
		mcp.WithString("content", mcp.Required(), mcp.Description("The content to remember")),
		mcp.WithString("type", mcp.Description(typeDescription(cfg, "Memory type to assign")), mcp.Enum(typeNames...)),
		mcp.WithArray("tags", mcp.Description("Tags for the memory"), mcp.WithStringItems()),
		mcp.WithObject("context", mcp.Description(
			"Optional capture-context fields as a flat key/value map. "+
				"Recognised keys include ticket, pr, project, related. "+
				"Values override any git auto-capture on the same key.",
		)),
	), h.remember)

	// memo_search
	srv.AddTool(mcp.NewTool("memo_search",
		mcp.WithDescription("Semantic search over memories"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 5)")),
		mcp.WithString("type", mcp.Description("Filter by memory type"), mcp.Enum(typeNames...)),
	), h.search)

	// memo_recall
	srv.AddTool(mcp.NewTool("memo_recall",
		mcp.WithDescription("Retrieve formatted context for LLM prompts"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Recall query")),
		mcp.WithNumber("limit", mcp.Description("Max memories (default 5)")),
	), h.recall)

	// memo_forget
	srv.AddTool(mcp.NewTool("memo_forget",
		mcp.WithDescription("Delete a memory by full UUID or 8-hex short-id (the prefix on Obsidian filenames)"),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithString("id", mcp.Required(), mcp.Description("Full UUID (36 chars) or 8-hex short-id prefix")),
	), h.forget)

	// memo_update
	srv.AddTool(mcp.NewTool("memo_update",
		mcp.WithDescription("Partially update a memory (re-embeds on content change)"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Memory ID to update")),
		mcp.WithString("content", mcp.Description("New content")),
		mcp.WithString("type", mcp.Description(typeDescription(cfg, "New memory type")), mcp.Enum(typeNames...)),
		mcp.WithArray("tags", mcp.Description("New tags"), mcp.WithStringItems()),
	), h.update)

	// memo_list
	srv.AddTool(mcp.NewTool("memo_list",
		mcp.WithDescription("List memories by recency"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
		mcp.WithString("type", mcp.Description("Filter by memory type"), mcp.Enum(typeNames...)),
	), h.list)

	// memo_similar
	srv.AddTool(mcp.NewTool("memo_similar",
		mcp.WithDescription("Find memories similar to given content (dedup helper)"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to compare")),
	), h.similar)

	// memo_reconcile
	srv.AddTool(mcp.NewTool("memo_reconcile",
		mcp.WithDescription("Reflect Obsidian deletes and type-folder moves back into the DB. Dry-run by default; set apply=true to commit."),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithBoolean("apply", mcp.Description("If true, commit the diff; otherwise preview only")),
	), h.reconcile)

	// memo_status
	srv.AddTool(mcp.NewTool("memo_status",
		mcp.WithDescription("Show memo inventory (counts per type, oldest/newest), configured paths, vault drift (orphans, type mismatches), embedding + LLM render config, and the most recent async LLM render error (if any)."),
		mcp.WithReadOnlyHintAnnotation(true),
	), h.status)

	stdioSrv := server.NewStdioServer(srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigChan
		cancel()
	}()

	return stdioSrv.Listen(ctx, os.Stdin, stdout)
}

func typeEnum(cfg *config.Config) []string {
	names := make([]string, len(cfg.Types))
	for i, t := range cfg.Types {
		names[i] = t.Name
	}
	return names
}

// typeDescription renders the configured types as a bulleted list so the LLM
// can pick the right one when assigning a memory's type. Built from
// cfg.Types[].Description, so editing ~/.memo/config.yaml and restarting
// `memo serve` updates what the agent sees.
func typeDescription(cfg *config.Config, prefix string) string {
	if len(cfg.Types) == 0 {
		return prefix
	}
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(". Options:")
	for _, t := range cfg.Types {
		b.WriteString("\n- ")
		b.WriteString(t.Name)
		if t.Description != "" {
			b.WriteString(": ")
			b.WriteString(t.Description)
		}
	}
	return b.String()
}

// --- Tool handlers ---

func (h *Handler) remember(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content := req.GetString("content", "")
	memType := req.GetString("type", "")
	tags := req.GetStringSlice("tags", nil)

	ctx := mergeCaptureContext(h.config, req.GetArguments()["context"])

	result, err := h.store.Store(content, tags, memType, ctx)
	if err != nil {
		logErr("memo_remember", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(result)
}

// mergeCaptureContext combines git auto-capture (from the server's cwd) with
// caller-supplied context. MCP-supplied values win over auto-captured ones on
// the same key. Returns nil when nothing was populated.
func mergeCaptureContext(cfg *config.Config, raw any) map[string]string {
	out := map[string]string{}
	if cfg != nil && cfg.CaptureContext() {
		if cwd, err := os.Getwd(); err == nil {
			for k, v := range capture.Git(cwd) {
				out[k] = v
			}
		}
	}
	if m, ok := raw.(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok && s != "" {
				out[k] = s
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *Handler) search(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	limit := req.GetInt("limit", 5)

	args := req.GetArguments()
	var typeFilter *string
	if v, ok := args["type"]; ok {
		if s, ok := v.(string); ok {
			typeFilter = &s
		}
	}

	results, err := h.store.Search(query, limit, typeFilter)
	if err != nil {
		logErr("memo_search", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(map[string]any{"memories": results})
}

func (h *Handler) recall(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	limit := req.GetInt("limit", 5)

	result, err := h.store.Recall(query, limit)
	if err != nil {
		logErr("memo_recall", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(result)
}

func (h *Handler) forget(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	idOrShort := req.GetString("id", "")

	id, err := h.store.ResolveID(idOrShort)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	result, err := h.store.Delete(id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(result)
}

func (h *Handler) reconcile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	apply := req.GetBool("apply", false)

	result, err := h.store.ReconcileVault(store.ReconcileOptions{Apply: apply})
	if err != nil {
		logErr("memo_reconcile", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(result)
}

func (h *Handler) update(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := req.GetString("id", "")

	args := req.GetArguments()

	var content *string
	if v, ok := args["content"]; ok {
		if s, ok := v.(string); ok {
			content = &s
		}
	}

	var tags *[]string
	if v, ok := args["tags"]; ok {
		if arr, ok := v.([]any); ok {
			t := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					t = append(t, s)
				}
			}
			tags = &t
		}
	}

	var memType *string
	if v, ok := args["type"]; ok {
		if s, ok := v.(string); ok {
			memType = &s
		}
	}

	result, err := h.store.Update(id, content, tags, memType)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(result)
}

func (h *Handler) list(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := req.GetInt("limit", 50)

	args := req.GetArguments()
	var typeFilter *string
	if v, ok := args["type"]; ok {
		if s, ok := v.(string); ok {
			typeFilter = &s
		}
	}

	results, err := h.store.List(limit, typeFilter)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(map[string]any{"memories": results})
}

func (h *Handler) similar(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content := req.GetString("content", "")

	results, err := h.store.FindSimilar(content)
	if err != nil {
		logErr("memo_similar", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(map[string]any{"memories": results})
}

func (h *Handler) status(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	info, err := h.store.Status()
	if err != nil {
		logErr("memo_status", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(info)
}
