# memo

A CLI memory tool with semantic search, powered by local embeddings. Store, search, and recall memories using natural language — all data stays on your machine.

Includes a native MCP server (`memo serve`) for direct integration with Claude Code and other MCP clients.

## Install

Requires Go 1.26+ and a C compiler (CGO is needed for sqlite-vec).

```bash
make install
```

The first run downloads the embedding model (~50MB) to `~/.memo/models/`.

## MCP Server

`memo serve` starts a long-running MCP server over stdio. This is the recommended way to use memo with Claude Code — one process stays warm with the embedding model loaded and DB connection open.

### Claude Code

```bash
claude mcp add memo -- memo serve
```

This registers memo as an MCP server. Claude Code will automatically discover and use these tools:

| Tool | Description |
|------|-------------|
| `memo_remember` | Store a memory with semantic dedup detection |
| `memo_search` | Semantic search over memories |
| `memo_recall` | Formatted context retrieval for LLM prompts |
| `memo_forget` | Delete a memory by ID |
| `memo_update` | Partial update (re-embeds on content change) |
| `memo_list` | List memories by recency |
| `memo_similar` | Find similar content (dedup helper) |

### Other MCP Clients

Any MCP client that supports stdio transport can connect:

```json
{
  "mcpServers": {
    "memo": {
      "command": "memo",
      "args": ["serve"]
    }
  }
}
```

### Why MCP over CLI?

Each CLI invocation (`memo search ...`) forks a process, loads the embedding model (~1s), opens SQLite, runs one operation, and exits. The MCP server keeps everything warm — tool calls complete in milliseconds instead of seconds.

## CLI Usage

Output adapts to context: human-readable colored cards in a terminal, JSON when piped or with `--json`.

```bash
memo --json search --query "kubernetes"   # Force JSON output
memo search --query "kubernetes"          # Cards in terminal, JSON when piped
```

Errors are always JSON: `{"error": "message"}`.

### remember — Store a memory

```bash
memo remember --content "K8s pods restart when OOMKilled" --type incident --tags "k8s,oom"
```

```json
{"id":"a1b2c3d4-...","status":"created"}
```

Duplicate detection is automatic:
- **Exact duplicate** (same content hash): returns `"status": "exists"`
- **Semantic duplicate** (cosine similarity >= 0.90): returns `"status": "similar_exists"`

### search — Semantic search

```bash
memo search --query "kubernetes memory issues" --limit 3
memo search --query "deployment patterns" --type architecture
```

Returns results ranked by cosine similarity (0.0–1.0).

### list — List memories

```bash
memo list
memo list --type fact --limit 10
```

Ordered by most recently updated.

### update — Modify a memory

```bash
memo update --id "a1b2c3d4-..." --tags "k8s,oom,resolved"
memo update --id "a1b2c3d4-..." --content "New content" --type learning
```

All fields are optional — only provided fields are updated. Embedding is regenerated on content change.

### recall — Get context for prompts

```bash
memo recall --query "Go concurrency patterns" --limit 3
```

```json
{
  "context": "1. [fact] Go uses goroutines...\n   Tags: go, concurrency\n   Score: 0.93",
  "memories": [...]
}
```

Returns a pre-formatted `context` string suitable for injecting into LLM prompts, plus the raw memory data.

### forget — Delete a memory

```bash
memo forget --id "a1b2c3d4-..."
```

### similar — Find duplicates

```bash
memo similar --content "goroutines vs threads"
```

Returns the 5 most similar existing memories with scores.

## Memory Types

Types are defined in config and validated at runtime. Unknown types are rejected with an error listing valid options.

| Type | Description |
|---|---|
| `note` | General observations, ideas, WIP thoughts (default) |
| `bug` | Bug reports, error patterns, known issues |
| `incident` | Production incidents, outages, escalations |
| `architecture` | Architecture decisions, system design patterns |
| `learning` | Lessons learned, insights, patterns |
| `fact` | Verified information, solutions, configs |

Add custom types by editing `~/.memo/config.yaml`.

## Configuration

Auto-created at `~/.memo/config.yaml` on first run:

```yaml
db_path: ~/.memo/memories.db
embedding:
  model: BAAI/bge-small-en-v1.5
  dimensions: 384
  cache_dir: ~/.memo/models
duplicate_threshold: 0.90

types:
  - name: note
    description: "General observations, ideas, WIP thoughts"
    default: true
  - name: bug
    description: "Bug reports, error patterns, known issues"
  - name: incident
    description: "Production incidents, outages, escalations"
  - name: architecture
    description: "Architecture decisions, system design patterns"
  - name: learning
    description: "Lessons learned, insights, patterns"
  - name: fact
    description: "Verified information, solutions, configs"
```

## Design

| Decision | Rationale |
|---|---|
| Pure Go embeddings (GoMLX) | No ONNX Runtime install required |
| sqlite-vec cosine distance | Proper 0–1 similarity scores for semantic search |
| SHA256 content-hashed IDs | Deterministic — same content always gets same ID, enabling dedup |
| Config-driven types | Extensible without recompilation, strict validation |
| Dual output (TTY cards / JSON) | Human-friendly in terminal, machine-parseable when piped |
| MCP stdio server | Single warm process — no per-call model load or DB open overhead |
