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
CLI (Cobra) → MemoryStore → DB (SQLite + sqlite-vec) + Embedder (hugot/GoMLX) → JSON stdout
```

- **cmd/** — One file per Cobra command. `root.go` loads config and manages store lifecycle via `PersistentPreRunE`/`PersistentPostRun`.
- **internal/store/** — Core business logic. Orchestrates embedding, two-tier duplicate detection, and DB operations.
- **internal/db/** — SQLite + sqlite-vec. Three tables: `memories` (content), `memories_vec` (float[384] cosine KNN), `memory_vectors` (ID↔rowid bridge). All mutations use transactions.
- **internal/embedding/** — Runs `BAAI/bge-small-en-v1.5` locally via hugot/GoMLX. Model auto-downloads on first use to `~/.memo/models/`.
- **internal/model/** — Data structs and SHA256→UUID deterministic ID generation.
- **internal/config/** — YAML config at `~/.memo/config.yaml`, auto-created on first run. Memory types are config-driven and validated at runtime.

## Key Design Decisions

- **Content-addressed IDs**: `model.GenerateID()` hashes content with SHA256, formats first 16 bytes as UUID. Exact-duplicate detection without DB lookup.
- **Two-tier dedup**: Hash match (`"exists"`) + cosine similarity >= 0.90 threshold (`"similar_exists"`).
- **All output is JSON** to stdout, including errors. Designed for machine consumption.
- **Embeddings re-generated on update**: Content changes always recompute the vector.
