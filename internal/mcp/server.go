package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/yuri-bondarenko/memo/internal/config"
	"github.com/yuri-bondarenko/memo/internal/store"
)

type Handler struct {
	store  *store.MemoryStore
	config *config.Config
}

func Serve(s *store.MemoryStore, cfg *config.Config) error {
	h := &Handler{store: s, config: cfg}

	srv := server.NewMCPServer("memo", "0.1.0",
		server.WithToolCapabilities(false),
	)

	typeNames := typeEnum(cfg)

	// memo_remember
	srv.AddTool(mcp.NewTool("memo_remember",
		mcp.WithDescription("Store a new memory with semantic dedup detection"),
		mcp.WithString("content", mcp.Required(), mcp.Description("The content to remember")),
		mcp.WithString("type", mcp.Description("Memory type"), mcp.Enum(typeNames...)),
		mcp.WithArray("tags", mcp.Description("Tags for the memory"), mcp.WithStringItems()),
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
		mcp.WithDescription("Delete a memory by ID"),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithString("id", mcp.Required(), mcp.Description("Memory ID to delete")),
	), h.forget)

	// memo_update
	srv.AddTool(mcp.NewTool("memo_update",
		mcp.WithDescription("Partially update a memory (re-embeds on content change)"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Memory ID to update")),
		mcp.WithString("content", mcp.Description("New content")),
		mcp.WithString("type", mcp.Description("New memory type"), mcp.Enum(typeNames...)),
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

	return server.ServeStdio(srv)
}

func typeEnum(cfg *config.Config) []string {
	names := make([]string, len(cfg.Types))
	for i, t := range cfg.Types {
		names[i] = t.Name
	}
	return names
}

// --- Tool handlers ---

func (h *Handler) remember(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content := req.GetString("content", "")
	memType := req.GetString("type", "")
	tags := req.GetStringSlice("tags", nil)

	result, err := h.store.Store(content, tags, memType)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(result)
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
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(results)
}

func (h *Handler) recall(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	limit := req.GetInt("limit", 5)

	result, err := h.store.Recall(query, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(result)
}

func (h *Handler) forget(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := req.GetString("id", "")

	result, err := h.store.Delete(id)
	if err != nil {
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
	return mcp.NewToolResultJSON(results)
}

func (h *Handler) similar(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content := req.GetString("content", "")

	results, err := h.store.FindSimilar(content)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultJSON(results)
}
