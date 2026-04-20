# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

All commands require `CGO_ENABLED=1` because `go-sqlite3` and `sqlite-vec` are C bindings.

```bash
make build                              # Build to bin/memo
make install                            # Install to ~/go/bin/memo
make test                               # Run all tests
CGO_ENABLED=1 go test ./internal/model  # Single package
CGO_ENABLED=1 go vet ./...              # Static analysis
```

## Architecture

CLI memory layer with local semantic search. All data on-machine at `~/.memo/`.

```
CLI (Cobra) ──→ MemoryStore → DB (SQLite + sqlite-vec) + Embedder (hugot/GoMLX) → Format (TTY cards / JSON)
MCP (stdio) ──↗         └─→ Vault (.md files under ~/.memo/vault/, one-way projection)
```

### Project structure

```
memo
├── main.go                          # Entry point → cmd.Execute()
├── cmd/
│   ├── root.go                      # Cobra root, config loading, store init, --json/TTY detection
│   ├── remember.go                  # Store with duplicate detection
│   ├── search.go                    # KNN semantic search
│   ├── list.go                      # List by recency, optional type filter
│   ├── forget.go                    # Delete by ID
│   ├── update.go                    # Partial update + re-embed
│   ├── recall.go                    # Formatted context for LLM prompts
│   ├── similar.go                   # Deduplication search
│   ├── export.go                    # Full Obsidian vault rebuild (--rename, --dry-run)
│   └── serve.go                     # MCP server over stdio
├── internal/
│   ├── config/config.go             # YAML config + type registry
│   ├── model/
│   │   ├── model.go                 # Memory structs, SHA256 ID generation
│   │   └── model_test.go            # ID determinism + UUID format tests
│   ├── db/db.go                     # SQLite + sqlite-vec (cosine KNN)
│   ├── embedding/embedding.go       # hugot GoMLX embedding pipeline
│   ├── format/
│   │   ├── format.go                # Human-readable terminal output (colored cards, relative time)
│   │   └── format_test.go           # Output formatting tests
│   ├── vault/
│   │   ├── vault.go                 # Obsidian vault projection: Sync, Delete, ExportAll, prune
│   │   ├── filename.go              # ShortID, Slugify, Title (NFC-normalized, ASCII-clamped)
│   │   ├── frontmatter.go           # YAML frontmatter rendering via yaml.v3
│   │   └── vault_test.go            # Filename, slug, frontmatter, move, prune tests
│   ├── mcp/server.go                # MCP server: tool registration + handlers
│   └── store/store.go               # MemoryStore business logic
└── Makefile
```

### Package responsibilities

- **cmd/** — One file per Cobra command. `root.go` loads config, constructs the vault, manages store lifecycle via `PersistentPreRunE`/`PersistentPostRun`, and provides `useJSON()` which auto-detects TTY via `go-isatty` (or honors `--json` flag).
- **internal/store/** — Core business logic. Orchestrates embedding, two-tier duplicate detection, DB operations, and post-write vault sync. `New(cfg, v)` accepts an optional `*vault.Vault`; nil disables export. `Store`/`Update`/`Delete` invoke the vault hook only on actual mutations; errors log to stderr but never fail the DB write.
- **internal/db/** — SQLite + sqlite-vec. Three tables: `memories` (content), `memories_vec` (float[384] cosine KNN), `memory_vectors` (ID↔rowid bridge). All mutations use transactions.
- **internal/embedding/** — Runs `BAAI/bge-small-en-v1.5` locally via hugot/GoMLX. Model auto-downloads on first use to `~/.memo/models/`.
- **internal/format/** — Human-readable terminal output. Colored type badges, relative timestamps (`3d ago`), short IDs, and score percentages. Uses `fatih/color`.
- **internal/vault/** — One-way projection of DB state into `~/.memo/vault/` as Markdown files. Layout is `<vault>/<type>/<short-id>-<slug>.md`; slugs are frozen at first write and filenames stay stable across content edits (glob lookup by short-id). `Sync` is the post-write hook; `ExportAll` runs full rebuilds with optional `Rename` and `DryRun`. Pruning is shape-aware: only files whose basename starts with an 8-hex short-id are candidates, so user-authored `.md` files survive.
- **internal/model/** — Data structs and SHA256→UUID deterministic ID generation.
- **internal/mcp/** — MCP server over stdio using `mcp-go`. Registers 7 tools that delegate to `MemoryStore`. Handles its own lifecycle via `cmd/serve.go` (bypasses root's `PersistentPreRunE`). Dynamic type enums from config. All errors returned as tool-level results, never protocol-level. Vault sync happens transparently through the store hook; there is no dedicated MCP tool.
- **internal/config/** — YAML config at `~/.memo/config.yaml`, auto-created on first run. Memory types are config-driven and validated at runtime. `vault_path` defaults to `~/.memo/vault/` and is backfilled for configs written before this field existed.

### Data flow

```
CLI:  flags → Cobra command → MemoryStore → DB + Embedder → (post-write) Vault hook → Format layer → stdout
MCP:  JSON-RPC (stdio) → mcp-go → Handler → MemoryStore → DB + Embedder → (post-write) Vault hook → JSON tool result
```

1. **Cobra** parses flags, validates required params
2. **MemoryStore** orchestrates business logic (dedup check, embedding, CRUD)
3. **Embedder** (hugot/GoMLX) converts text → 384-dim float32 vector, pure Go inference
4. **DB** (SQLite + sqlite-vec) stores memories and runs cosine KNN search
5. **Vault** projects each mutation into a `.md` file under `~/.memo/vault/<type>/`; failures log to stderr and do not propagate
6. **Format** renders output — `useJSON()` checks `--json` flag or TTY via `go-isatty`; terminal gets colored cards with relative timestamps, pipes get JSON

### Database schema

Three tables in `~/.memo/memories.db`:

- **`memories`** — content, type, tags, timestamps
- **`memories_vec`** — sqlite-vec virtual table with cosine distance, stores float[384] embeddings
- **`memory_vectors`** — bridges memory IDs to vector rowids

### Dependencies

| Package | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGO) |
| `github.com/asg017/sqlite-vec-go-bindings/cgo` | Vector search extension |
| `github.com/knights-analytics/hugot` | Embedding pipeline (GoMLX backend) |
| `github.com/fatih/color` | Colored terminal output |
| `github.com/mattn/go-isatty` | TTY detection for output mode |
| `gopkg.in/yaml.v3` | Config parsing, vault frontmatter rendering |
| `golang.org/x/text/unicode/norm` | NFC normalization for vault slug generation |
| `github.com/mark3labs/mcp-go` | MCP server (stdio transport, tool registration) |

## Key Design Decisions

- **Content-addressed IDs**: `model.GenerateID()` hashes content with SHA256, formats first 16 bytes as UUID. Exact-duplicate detection without DB lookup.
- **Two-tier dedup**: Hash match (`"exists"`) + cosine similarity >= 0.90 threshold (`"similar_exists"`).
- **Dual output**: Human-readable colored cards in TTY, JSON when piped or `--json`. Errors are always JSON to stdout.
- **Embeddings re-generated on update**: Content changes always recompute the vector.
- **MCP server lifecycle**: `cmd/serve.go` overrides root's `PersistentPreRunE` with a no-op and manages its own `store.New()` + `defer Close()`. The store stays warm for the entire stdio session — single model load, persistent DB connection.
- **MCP error convention**: All handlers return `(result, nil)`. Business errors use `mcp.NewToolResultError()` so Claude sees error text as tool output, keeping the JSON-RPC protocol clean.
- **Vault is a one-way projection**: DB is the sole source of truth. `remember`/`update`/`forget` auto-sync through the store's post-write hook; edits made inside Obsidian are silently overwritten on the next sync. This sidesteps conflict resolution and keeps content-addressed IDs sound (bidirectional sync would require decoupling logical identity from content hash).
- **Frozen slug + glob lookup**: Filename is `<short-id>-<slug>.md` with `<short-id>` being the first 8 hex chars of the UUID. The slug is generated at first write and never automatically regenerated (`memo export --rename` is the explicit opt-in). Existing files are found by globbing `<short-id>-*.md` across type folders, so content edits never rename files and Obsidian wikilinks remain stable.
- **Vault errors never fail writes**: `syncVault`/`deleteVault` log to stderr and swallow errors. A vault outage (iCloud offline, disk full) must never prevent a memory from being stored in the DB.
