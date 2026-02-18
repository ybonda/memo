# memo

A CLI memory tool with semantic search, powered by local embeddings. Store, search, and recall memories using natural language — all data stays on your machine.

Includes a native MCP server (`memo serve`) for direct integration with Claude Code and other MCP clients.

## Quick Start

### 1. Direct CLI usage

Store memories, search them semantically, and recall context — all from the terminal.

```bash
# Store a few memories
memo remember --content "Go uses goroutines and channels for concurrency" --type fact --tags "go,concurrency,channels"
memo remember --content "Always validate user input at API boundaries to prevent injection attacks" --type learning --tags "security,api"
memo remember --content "MongoDB Atlas supports global distribution across regions" --type architecture --tags "mongodb,database"

# Semantic search — finds relevant memories even with different wording
memo search --query "parallel programming in Go"

# List everything you've stored
memo list

# Get formatted context you can paste into an LLM prompt
memo recall --query "database scaling"

# Find near-duplicates before storing
memo similar --content "Go channels enable CSP-style concurrency"

# Update or remove
memo update --id 31940748 --tags "go,concurrency,channels,goroutines"
memo forget --id 80035334
```

In a terminal, output renders as colored cards with relative timestamps:

```
[note] this is my new mem note
  updated: just now  ·  id: 425b2d77

[fact] Go uses goroutines and channels for concurrency
  tags: go, concurrency, channels  ·  updated: 1d ago  ·  id: 31940748

[learning] Always validate user input at API boundaries to prevent injection attacks
  tags: security, api  ·  updated: 1d ago  ·  id: 428cee7e

[architecture] MongoDB Atlas supports global distribution across regions
  tags: mongodb, database  ·  updated: 1d ago  ·  id: ce7429b6
```

When piped or with `--json`, output is machine-readable JSON.

### 2. Inside Claude Code / Cursor via MCP

`memo serve` runs memo as a long-running MCP server over stdio. Claude Code, Cursor, and other MCP-compatible tools can call memo's tools directly — no shell forking, no model reload per call.

**Setup (one command):**

```bash
# Claude Code — add globally so memo is available in every project
claude mcp add --scope user memo -- memo serve

# Cursor — add to .cursor/mcp.json in your project
```

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

**What it looks like in practice:**

Once configured, your AI assistant can store and retrieve memories automatically. For example, in Claude Code:

```
You:  "Remember that our API rate limit is 100 req/s per tenant"
       → Claude calls memo_remember with type=fact, tags=api,rate-limit

You:  "What do we know about rate limiting?"
       → Claude calls memo_search with query="rate limiting"
       → Gets back relevant memories with similarity scores

You:  "Summarize what we've learned about our Go services"
       → Claude calls memo_recall with query="Go services"
       → Gets pre-formatted context injected into its prompt
```

All seven tools are available to the assistant:

| Tool | What it does |
|------|-------------|
| `memo_remember` | Store a memory (auto-detects duplicates) |
| `memo_search` | Semantic search across all memories |
| `memo_recall` | Get formatted context for LLM prompts |
| `memo_list` | List memories by recency |
| `memo_similar` | Find near-duplicates |
| `memo_update` | Update content, type, or tags |
| `memo_forget` | Delete a memory by ID |

> **Why MCP over CLI?** Each CLI invocation forks a process, loads the embedding model (~1s), opens SQLite, runs one operation, and exits. The MCP server keeps everything warm — tool calls complete in milliseconds instead of seconds.

---

## Install

Requires Go 1.26+ and a C compiler (CGO is needed for sqlite-vec).

```bash
make install
```

The first run downloads the embedding model (~50MB) to `~/.memo/models/`.

## CLI Reference

Output adapts automatically: colored cards in a terminal, JSON when piped or with `--json`. Errors are always JSON.

```bash
memo --json search --query "kubernetes"   # Force JSON output
memo search --query "kubernetes"          # Cards in terminal, JSON when piped
```

| Command | Flags | Notes |
|---------|-------|-------|
| `memo remember` | `--content` (required), `--type`, `--tags` | Auto-dedup: returns `"exists"` (exact hash) or `"similar_exists"` (cosine >= 0.90) |
| `memo search` | `--query` (required), `--type`, `--limit` | Ranked by cosine similarity (0.0–1.0) |
| `memo list` | `--type`, `--limit` | Ordered by most recently updated |
| `memo recall` | `--query` (required), `--limit` | Returns pre-formatted `context` string + raw memory data |
| `memo similar` | `--content` (required) | Returns 5 most similar memories with scores |
| `memo update` | `--id` (required), `--content`, `--type`, `--tags` | Only provided fields change; re-embeds on content change |
| `memo forget` | `--id` (required) | Permanent delete |
| `memo serve` | | Starts MCP server over stdio (see Quick Start) |

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
