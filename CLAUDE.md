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
│   ├── reconcile.go                 # Apply Obsidian deletes and type-folder moves to the DB
│   ├── status.go                    # Stats, paths, vault drift, last async render error
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
│   ├── llm/
│   │   ├── llm.go                   # Optional `claude -p` subprocess renderer for Obsidian-polished markdown
│   │   ├── prompt.go                # Hardcoded system prompt for the render pass
│   │   └── llm_test.go              # Renderer interface + postprocess tests
│   ├── vault/
│   │   ├── vault.go                 # Obsidian vault projection: Sync, Delete, ExportAll, WalkManaged, prune
│   │   ├── filename.go              # ShortID, Slugify, Title (NFC-normalized, ASCII-clamped)
│   │   ├── frontmatter.go           # YAML frontmatter rendering via yaml.v3; body runs through Format
│   │   ├── formatter.go             # Deterministic markdown shaping: bold lead-ins, (N)→list, backticks
│   │   ├── formatter_test.go        # Table-driven formatter tests + incident golden
│   │   └── vault_test.go            # Filename, slug, frontmatter, move, prune, WalkManaged tests
│   ├── mcp/server.go                # MCP server: tool registration + handlers
│   └── store/store.go               # MemoryStore business logic
└── Makefile
```

### Package responsibilities

- **cmd/** — One file per Cobra command. `root.go` loads config, constructs the vault, manages store lifecycle via `PersistentPreRunE`/`PersistentPostRun`, and provides `useJSON()` which auto-detects TTY via `go-isatty` (or honors `--json` flag).
- **internal/store/** — Core business logic. Orchestrates embedding, two-tier duplicate detection, DB operations, and post-write vault sync. `New(cfg, v)` accepts an optional `*vault.Vault`; nil disables export. `Store`/`Update`/`Delete` invoke the vault hook only on actual mutations; errors log to stderr but never fail the DB write. When `llm_md_export` is enabled, `Store`/`Update` schedule the LLM render in a background goroutine via `scheduleRender` after the DB row is already persisted — the caller returns in milliseconds while the vault gets upgraded from deterministic to LLM-polished markdown asynchronously. In-flight renders are tracked by `renderWG` and drained by `Close()`. Stale renders (from a rapid `remember` → `update` race) are dropped via a `UpdatedAt` guard before writing `RenderedBody`. `scheduleRender` also stashes any render failure in a mutex-guarded `lastRenderErr` slot; `LastRenderError()` returns a copy so agents and users can see why the polished body is missing. `ReformatOne` stays synchronous — callers asking for an explicit re-render want to wait. `ResolveID` accepts either a full UUID or an 8-hex short-id (with an optional `.md` suffix) and returns the canonical full UUID. `ReconcileVault` diffs the vault against the DB and, with `Apply=true`, commits deletes and type changes directly via `db.Delete`/`db.UpdateType` — the `syncVault`/`deleteVault` hooks are deliberately bypassed so the authoritative filesystem state is not echoed back. `Status()` aggregates `db.CountAll`/`CountByType`/`CreatedRange`, file-size stats on the main DB and its WAL/SHM sidecars, a dry-run `ReconcileVault` for vault drift, the config-declared embedding and LLM render settings, and `LastRenderError()` into a single `*StatusInfo` used by both `memo status` and the `memo_status` MCP tool.
- **internal/db/** — SQLite + sqlite-vec. Three tables: `memories` (content), `memories_vec` (float[384] cosine KNN), `memory_vectors` (ID↔rowid bridge). All mutations use transactions.
- **internal/embedding/** — Runs `BAAI/bge-small-en-v1.5` locally via hugot/GoMLX. Model auto-downloads on first use to `~/.memo/models/`.
- **internal/format/** — Human-readable terminal output. Colored type badges, relative timestamps (`3d ago`), short IDs, and score percentages. Uses `fatih/color`.
- **internal/llm/** — Optional vault-polish feature. `ClaudeCLI.Render` shells out to `claude -p --model <model>` (hardcoded prompt from `prompt.go`) to turn raw memory content into richer Obsidian markdown (callouts, wikilinked ticket IDs, proper headings). The package intentionally has no dependency on `anthropic-sdk-go`: subprocess invocation leverages the user's Claude Code subscription so there is no per-token billing. A `nil` renderer means the feature is disabled; callers check for that rather than branching on config. The renderer is invoked exclusively from the store's async `scheduleRender` — never on the write path.
- **internal/vault/** — Projection of DB state into `~/.memo/vault/` as Markdown files. Layout is `<vault>/<type>/<short-id>-<slug>.md`; slugs are frozen at first write and filenames stay stable across content edits (glob lookup by short-id). `Sync` is the post-write hook; `ExportAll` runs full rebuilds with optional `Rename` and `DryRun`. `WalkManaged` enumerates every `.md` file whose basename matches the short-id shape, grouped by type folder, so the store's reconcile pass can diff vault against DB. `Render` pipes the memory body through `Format` before emitting the `.md` file; `Format` adds deterministic markdown structure without mutating the raw content stored in the DB. Pruning and reconcile are both shape-aware: only files whose basename starts with an 8-hex short-id are candidates, so user-authored `.md` files survive.
- **internal/model/** — Data structs and SHA256→UUID deterministic ID generation.
- **internal/mcp/** — MCP server over stdio using `mcp-go`. Registers 9 tools that delegate to `MemoryStore`. Handles its own lifecycle via `cmd/serve.go` (bypasses root's `PersistentPreRunE`). Dynamic type enums from config. All errors returned as tool-level results, never protocol-level. The `memo_forget` handler routes its `id` argument through `store.ResolveID`, so short-id prefixes (the 8 hex chars on Obsidian filenames) are accepted. The `memo_reconcile` tool surfaces the vault-to-DB diff to the agent; `apply=false` is a dry-run, `apply=true` commits. The `memo_status` tool is read-only and takes no arguments; it returns the same `*StatusInfo` JSON that `memo status --json` emits.
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
- **Vault sync is split by concern**: content is one-way (DB → vault), structural metadata is two-way but only on demand. `remember`/`update`/`forget` auto-sync their `.md` file through the store's post-write hook; body edits made inside Obsidian are silently overwritten on the next sync (this sidesteps conflict resolution and keeps content-addressed IDs sound). Two structural edits made inside Obsidian are authoritative but applied only when the user runs `memo reconcile`: **deleting a file** (triggers `db.Delete`) and **moving a file to a different type folder** (triggers `db.UpdateType`). Reconcile's apply path calls `db.Delete`/`db.UpdateType` directly and skips `syncVault`/`deleteVault` so the filesystem state does not echo back onto itself. Files whose short-id matches no DB row are collected as `Unknown` and never auto-deleted.
- **LLM render is async and vault-only**: When `llm_md_export.enabled: true`, `Store`/`Update` fire the `claude -p` subprocess in a background goroutine *after* the DB row is persisted and the vault `.md` file has been written with the deterministic body. `memo_remember` / `memo_update` return in milliseconds; the goroutine overwrites `RenderedBody` in the DB and re-syncs the vault `.md` when Sonnet finishes (~60-120s for a large memo). This keeps Claude Code unblocked on incident memos and similar long-form writes. Stale renders from a `remember` → rapid `update` race are filtered by re-fetching the memory inside the goroutine and comparing `UpdatedAt` before writing. `Close()` drains `renderWG` so pending renders land before process exit, which matters more for MCP server shutdown than short-lived CLI commands (CLI users who need the polish synchronously can run `memo reformat <id>` after).
- **Formatter is deterministic and always-on**: `internal/vault/formatter.go:Format` is a pure function invoked by `Render` before the body is written. It adds structural markdown only (bolded ALL-CAPS lead-in labels, real numbered lists from inline `(N)` enumerations, paragraph splits at sentence boundaries, backticks on hyphenated-lowercase identifiers) without touching `m.Content`. The raw content in the DB remains the single source for SHA256 IDs and embeddings. Short inputs (< 200 runes) and inputs that already contain structural markdown (blank lines, headings, lists, bold, or fenced code) short-circuit to unchanged, so hand-written markdown passes through byte-for-byte and the formatter is idempotent on its own output.
- **Frozen slug + glob lookup**: Filename is `<short-id>-<slug>.md` with `<short-id>` being the first 8 hex chars of the UUID. The slug is generated at first write and never automatically regenerated (`memo export --rename` is the explicit opt-in). Existing files are found by globbing `<short-id>-*.md` across type folders, so content edits never rename files and Obsidian wikilinks remain stable.
- **Vault errors never fail writes**: `syncVault`/`deleteVault` log to stderr and swallow errors. A vault outage (iCloud offline, disk full) must never prevent a memory from being stored in the DB.
- **Async render errors are surfaced, not swallowed**: `scheduleRender` keeps the existing stderr log but also stashes the most recent failure (timestamp, memory id, type, error string) in a mutex-guarded single slot on `MemoryStore`. `Status()` reads it through `LastRenderError()`, so `memo status` and `memo_status` can show "did anything silently fail recently?" without needing a log file or an error channel. Single-slot by design: status is a health signal, not a history; older failures fall off when a newer one lands.
