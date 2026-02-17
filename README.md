# memo

A CLI memory tool with semantic search, powered by local embeddings. Store, search, and recall memories using natural language — all data stays on your machine.

Built as a Go port of memento (Rust MCP server), designed for direct CLI invocation from tools like Claude Code.

## Install

Requires Go 1.24+ and a C compiler (CGO is needed for sqlite-vec).

```bash
make install
```

Or install from source:

```bash
CGO_ENABLED=1 go install github.com/ybonda/memo@latest
```

Or build from source:

```bash
git clone https://github.com/ybonda/memo.git && cd memo && make build  # binary at bin/memo
```

The first run downloads the embedding model (~50MB) to `~/.memo/models/`.

## Usage

All output is JSON to stdout. Errors are also JSON: `{"error": "message"}`.

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

## Architecture

```
memo
├── main.go                          # Entry point → cmd.Execute()
├── cmd/
│   ├── root.go                      # Cobra root, config loading, store init
│   ├── remember.go                  # Store with duplicate detection
│   ├── search.go                    # KNN semantic search
│   ├── list.go                      # List by recency, optional type filter
│   ├── forget.go                    # Delete by ID
│   ├── update.go                    # Partial update + re-embed
│   ├── recall.go                    # Formatted context for LLM prompts
│   └── similar.go                   # Deduplication search
├── internal/
│   ├── config/config.go             # YAML config + type registry
│   ├── model/model.go               # Memory structs, SHA256 ID generation
│   ├── db/db.go                     # SQLite + sqlite-vec (cosine KNN)
│   ├── embedding/embedding.go       # hugot GoMLX embedding pipeline
│   └── store/store.go               # MemoryStore business logic
└── Makefile
```

### Data flow

```
CLI flags → Cobra command → MemoryStore → DB + Embedder → JSON stdout
```

1. **Cobra** parses flags, validates required params
2. **MemoryStore** orchestrates business logic (dedup check, embedding, CRUD)
3. **Embedder** (hugot/GoMLX) converts text → 384-dim float32 vector, pure Go inference
4. **DB** (SQLite + sqlite-vec) stores memories and runs cosine KNN search
5. **JSON** result written to stdout

### Database schema

Three tables in `~/.memo/memories.db`:

- **`memories`** — content, type, tags, timestamps
- **`memories_vec`** — sqlite-vec virtual table with cosine distance, stores float[384] embeddings
- **`memory_vectors`** — bridges memory IDs to vector rowids

### Key design decisions

| Decision | Rationale |
|---|---|
| Pure Go embeddings (GoMLX) | No ONNX Runtime install required |
| sqlite-vec cosine distance | Proper 0–1 similarity scores for semantic search |
| SHA256 content-hashed IDs | Deterministic — same content always gets same ID, enabling dedup |
| Config-driven types | Extensible without recompilation, strict validation |
| JSON-only output | Machine-parseable for tool integration |

### Dependencies

| Package | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGO) |
| `github.com/asg017/sqlite-vec-go-bindings/cgo` | Vector search extension |
| `github.com/knights-analytics/hugot` | Embedding pipeline (GoMLX backend) |
| `gopkg.in/yaml.v3` | Config parsing |
