package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ybonda/memo/internal/config"
	"github.com/ybonda/memo/internal/db"
	"github.com/ybonda/memo/internal/embedding"
	"github.com/ybonda/memo/internal/llm"
	"github.com/ybonda/memo/internal/model"
	"github.com/ybonda/memo/internal/vault"
)

// ReconcileOptions controls a vault-to-DB reconcile pass.
type ReconcileOptions struct {
	// Apply commits the diff. Without it, ReconcileVault is purely read-only
	// (dry-run) and callers can preview intended changes.
	Apply bool
}

// DeleteItem describes a memory the reconcile pass would delete.
type DeleteItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// TypeChange describes a memory whose type folder has changed in the vault.
type TypeChange struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	OldType string `json:"old_type"`
	NewType string `json:"new_type"`
}

// ReconcileResult summarizes a reconcile pass.
type ReconcileResult struct {
	Applied     bool         `json:"applied"`
	ToDelete    []DeleteItem `json:"to_delete"`
	TypeChanges []TypeChange `json:"type_changes"`
	Unknown     []string     `json:"unknown"`
}

// listAllCap is the effective upper bound used when the store needs to
// iterate every memory (e.g. full vault export). SQLite's LIMIT requires a
// concrete integer; this is large enough to cover any realistic memory
// collection.
const listAllCap = 1 << 30

type MemoryStore struct {
	db       *db.DB
	embedder *embedding.Embedder
	config   *config.Config
	vault    *vault.Vault // optional; nil disables vault export
	renderer llm.Renderer // optional; nil when LLMExport.Enabled is false

	// renderWG tracks in-flight async LLM renders. Close() drains it so
	// pending work gets a chance to persist before shutdown.
	renderWG sync.WaitGroup

	// lastRenderErr remembers the most recent async LLM render failure so
	// `memo status` and `memo_status` can surface it. Single-slot by design:
	// status is a "did anything break" signal, not a full log. nil when no
	// failures have occurred in this process.
	lastRenderErrMu sync.Mutex
	lastRenderErr   *StatusRenderError
}

// New constructs a MemoryStore. The vault is optional — pass nil to disable
// Obsidian export (useful in tests or tools that should not touch the
// filesystem beyond the DB). The LLM renderer is configured automatically
// from cfg.LLMExport; when disabled, the deterministic vault formatter runs
// unchanged.
func New(cfg *config.Config, v *vault.Vault) (*MemoryStore, error) {
	database, err := db.Open(cfg.DBPath, cfg.Embedding.Dimensions)
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	embedder, err := embedding.New(cfg.Embedding.Model, cfg.Embedding.CacheDir)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("cannot init embedder: %w", err)
	}

	// Warmup: force model weights into memory and verify inference works.
	if _, err := embedder.Embed("warmup"); err != nil {
		embedder.Destroy()
		database.Close()
		return nil, fmt.Errorf("embedder warmup failed: %w", err)
	}

	var renderer llm.Renderer
	if r := llm.NewFromConfig(cfg.LLMExport); r != nil {
		renderer = r
	}

	return &MemoryStore{
		db:       database,
		embedder: embedder,
		config:   cfg,
		vault:    v,
		renderer: renderer,
	}, nil
}

// scheduleRender fires the LLM render in a background goroutine after the DB
// row is already persisted. The goroutine re-fetches the memory before writing
// RenderedBody so a subsequent Update that advances UpdatedAt invalidates
// stale renders rather than letting them overwrite fresher content. Errors
// are logged but never propagate: the caller has already returned success.
//
// No-op when no renderer is configured. Callers must invoke this AFTER the
// initial db.Insert/db.Update + syncVault so the vault always has a valid
// deterministic body even if the LLM pass never completes (short-lived CLI
// processes, timeouts, etc).
func (s *MemoryStore) scheduleRender(id, content, updatedAt string) {
	if s.renderer == nil {
		return
	}
	s.renderWG.Go(func() {
		rendered, err := s.renderer.Render(context.Background(), content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[memo] async llm render failed for %s: %v\n", id, err)
			s.recordRenderError(id, err)
			return
		}
		cur, err := s.db.Get(id)
		if err != nil || cur == nil {
			return
		}
		if cur.UpdatedAt != updatedAt {
			return // memory mutated mid-render; drop stale output
		}
		by := s.renderer.Model()
		if err := s.db.UpdateRenderedBody(id, rendered, by); err != nil {
			fmt.Fprintf(os.Stderr, "[memo] async llm db update failed for %s: %v\n", id, err)
			s.recordRenderError(id, err)
			return
		}
		cur.RenderedBody = rendered
		cur.RenderedBy = by
		s.syncVault(cur)
	})
}

// recordRenderError stashes a copy of a render-time failure on the store for
// later retrieval by Status(). The memory type is looked up opportunistically
// so the eventual status card can show which memory failed in context.
func (s *MemoryStore) recordRenderError(id string, renderErr error) {
	var memType string
	if m, _ := s.db.Get(id); m != nil {
		memType = m.Type
	}
	s.lastRenderErrMu.Lock()
	s.lastRenderErr = &StatusRenderError{
		When:       time.Now().UTC(),
		MemoryID:   id,
		MemoryType: memType,
		Error:      renderErr.Error(),
	}
	s.lastRenderErrMu.Unlock()
}

// LastRenderError returns a copy of the most recent async render failure, or
// nil if none has occurred. A copy is returned so callers cannot hold a
// reference to the mutex-guarded slot.
func (s *MemoryStore) LastRenderError() *StatusRenderError {
	s.lastRenderErrMu.Lock()
	defer s.lastRenderErrMu.Unlock()
	if s.lastRenderErr == nil {
		return nil
	}
	cp := *s.lastRenderErr
	return &cp
}

// FlushRenders blocks until all in-flight async renders complete. Close()
// calls it automatically; exposed for tests or callers (e.g. CLI paths) that
// need the vault to be LLM-finalized before proceeding.
func (s *MemoryStore) FlushRenders() {
	s.renderWG.Wait()
}

// Vault returns the underlying vault handle, or nil if none is attached.
func (s *MemoryStore) Vault() *vault.Vault { return s.vault }

// ExportVault rewrites the entire vault from the current DB state. Used by
// the `memo export` command.
func (s *MemoryStore) ExportVault(opts vault.ExportOptions) (*vault.ExportResult, error) {
	if s.vault == nil {
		return nil, fmt.Errorf("vault is not configured")
	}
	mems, err := s.db.ListAll(listAllCap, nil)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	return s.vault.ExportAll(mems, opts)
}

// syncVault runs the single-memory post-write hook. Errors are logged but not
// propagated — a vault failure must never fail the DB write.
func (s *MemoryStore) syncVault(m *model.Memory) {
	if s.vault == nil {
		return
	}
	if err := s.vault.Sync(m); err != nil {
		fmt.Fprintf(os.Stderr, "[memo] vault sync failed for %s: %v\n", m.ID, err)
	}
}

func (s *MemoryStore) deleteVault(id string) {
	if s.vault == nil {
		return
	}
	if err := s.vault.Delete(id); err != nil {
		fmt.Fprintf(os.Stderr, "[memo] vault delete failed for %s: %v\n", id, err)
	}
}

// Close drains any in-flight async LLM renders, then tears down the DB and
// embedder. Drain guarantees that renders scheduled during normal operation
// (MCP server lifetime, CLI command execution) have a chance to land in the
// DB and vault before the process exits. Goroutines are cheap; this is a
// simple wait rather than a timeout because the renderer already enforces its
// own per-call timeout (llm_md_export.timeout_seconds in config.yaml).
func (s *MemoryStore) Close() {
	s.renderWG.Wait()
	if s.db != nil {
		s.db.Close()
	}
	if s.embedder != nil {
		s.embedder.Destroy()
	}
}

// Store persists a new memory. The optional ctx map carries capture-time
// context (e.g. git branch, ticket id, project) that is written into
// frontmatter but deliberately NOT fed into model.GenerateID or the embedding
// — IDs and semantic dedup stay a pure function of Content, so the same raw
// text captured twice in different contexts still deduplicates.
func (s *MemoryStore) Store(content string, tags []string, memType string, ctx map[string]string) (*model.StoreResult, error) {
	if memType == "" {
		memType = s.config.DefaultType
	}
	if err := s.config.ValidateType(memType); err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}

	id := model.GenerateID(content)

	exists, err := s.db.Exists(id)
	if err != nil {
		return nil, err
	}
	if exists {
		return &model.StoreResult{ID: id, Status: "exists"}, nil
	}

	emb, err := s.embedder.Embed(content)
	if err != nil {
		return nil, err
	}

	similar, err := s.db.KNNSearch(emb, 1, nil)
	if err != nil {
		return nil, err
	}
	if len(similar) > 0 && similar[0].Score >= s.config.DuplicateThreshold {
		return &model.StoreResult{
			ID:            similar[0].ID,
			Status:        "similar_exists",
			SimilarMemory: &similar[0].Content,
		}, nil
	}

	now := model.NowRFC3339()
	mem := &model.Memory{
		ID:        id,
		Content:   content,
		Type:      memType,
		Tags:      tags,
		Context:   ctx,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.db.Insert(mem, emb); err != nil {
		return nil, err
	}

	s.syncVault(mem)
	s.scheduleRender(mem.ID, mem.Content, mem.UpdatedAt)
	return &model.StoreResult{
		ID:        id,
		ShortID:   vault.ShortID(id),
		Status:    "created",
		Type:      memType,
		SizeBytes: len(content),
		TagCount:  len(tags),
	}, nil
}

func (s *MemoryStore) Search(query string, limit int, memType *string) ([]model.MemoryWithScore, error) {
	if limit <= 0 {
		limit = 5
	}
	if memType != nil {
		if err := s.config.ValidateType(*memType); err != nil {
			return nil, err
		}
	}

	emb, err := s.embedder.Embed(query)
	if err != nil {
		return nil, err
	}
	return s.db.KNNSearch(emb, limit, memType)
}

func (s *MemoryStore) Delete(id string) (*model.DeleteResult, error) {
	deleted, err := s.db.Delete(id)
	if err != nil {
		return nil, err
	}
	if deleted {
		s.deleteVault(id)
	}
	return &model.DeleteResult{Deleted: deleted, ID: id}, nil
}

func (s *MemoryStore) Update(id string, content *string, tags *[]string, memType *string) (*model.UpdateResult, error) {
	mem, err := s.db.Get(id)
	if err != nil {
		return nil, err
	}
	if mem == nil {
		return &model.UpdateResult{Updated: false}, nil
	}

	contentChanged := false
	if content != nil && *content != mem.Content {
		mem.Content = *content
		contentChanged = true
	}
	if tags != nil {
		mem.Tags = *tags
	}
	if memType != nil {
		if err := s.config.ValidateType(*memType); err != nil {
			return nil, err
		}
		mem.Type = *memType
	}
	mem.UpdatedAt = model.NowRFC3339()

	// Clear cached rendered body and model attribution when content changed
	// so the vault falls back to the deterministic Format() until the async
	// render finishes. Without this, syncVault below would emit stale LLM
	// output tagged with a stale model for the new content.
	if contentChanged {
		mem.RenderedBody = ""
		mem.RenderedBy = ""
	}

	emb, err := s.embedder.Embed(mem.Content)
	if err != nil {
		return nil, err
	}

	if err := s.db.Update(mem, emb); err != nil {
		return nil, err
	}

	s.syncVault(mem)
	if contentChanged {
		s.scheduleRender(mem.ID, mem.Content, mem.UpdatedAt)
	}
	return &model.UpdateResult{Updated: true, Memory: mem}, nil
}

func (s *MemoryStore) List(limit int, memType *string) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	if memType != nil {
		if err := s.config.ValidateType(*memType); err != nil {
			return nil, err
		}
	}
	return s.db.ListAll(limit, memType)
}

// ResolveID resolves either a full UUID or an 8-hex short-id (or a filename
// containing one) to the canonical full UUID. An optional ".md" suffix is
// stripped so callers can pass an Obsidian filename directly.
func (s *MemoryStore) ResolveID(idOrShort string) (string, error) {
	trim := strings.TrimSpace(idOrShort)
	trim = strings.TrimSuffix(trim, ".md")
	if len(trim) == 36 {
		exists, err := s.db.Exists(trim)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", fmt.Errorf("no memory with id %s", trim)
		}
		return trim, nil
	}
	if len(trim) < 8 {
		return "", fmt.Errorf("id %q too short: need 8-char short-id or 36-char UUID", idOrShort)
	}
	short := trim[:8]
	for _, r := range short {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return "", fmt.Errorf("id %q must be hex", idOrShort)
		}
	}
	m, err := s.db.FindByShortID(short)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", fmt.Errorf("no memory matching %q", idOrShort)
	}
	return m.ID, nil
}

// ReconcileVault compares the vault on disk against the DB and reports which
// memories should be deleted (files missing) and which should change type
// (files in a different type folder). With opts.Apply=true, the diff is
// committed directly to the DB — bypassing syncVault/deleteVault since the
// filesystem is already authoritative for this class of change. Files whose
// short-id does not correspond to any DB row are collected as Unknown and
// never auto-deleted.
func (s *MemoryStore) ReconcileVault(opts ReconcileOptions) (*ReconcileResult, error) {
	if s.vault == nil {
		return nil, fmt.Errorf("vault is not configured")
	}
	files, err := s.vault.WalkManaged()
	if err != nil {
		return nil, fmt.Errorf("walk vault: %w", err)
	}
	mems, err := s.db.ListAll(listAllCap, nil)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	vaultByShort := make(map[string]vault.ManagedFile, len(files))
	for _, f := range files {
		vaultByShort[f.ShortID] = f
	}
	dbByShort := make(map[string]*model.Memory, len(mems))
	for i := range mems {
		dbByShort[vault.ShortID(mems[i].ID)] = &mems[i]
	}

	res := &ReconcileResult{
		ToDelete:    []DeleteItem{},
		TypeChanges: []TypeChange{},
		Unknown:     []string{},
	}

	for i := range mems {
		m := &mems[i]
		short := vault.ShortID(m.ID)
		f, ok := vaultByShort[short]
		if !ok {
			res.ToDelete = append(res.ToDelete, DeleteItem{
				ID: m.ID, Title: vault.Title(m.Content),
			})
			continue
		}
		if f.TypeFolder != "" && f.TypeFolder != m.Type {
			if err := s.config.ValidateType(f.TypeFolder); err != nil {
				fmt.Fprintf(os.Stderr, "[memo] reconcile: skipping %s: moved to unknown type %q (%v)\n",
					short, f.TypeFolder, err)
				continue
			}
			res.TypeChanges = append(res.TypeChanges, TypeChange{
				ID: m.ID, Title: vault.Title(m.Content),
				OldType: m.Type, NewType: f.TypeFolder,
			})
		}
	}

	for short := range vaultByShort {
		if _, ok := dbByShort[short]; !ok {
			res.Unknown = append(res.Unknown, short)
		}
	}
	sort.Strings(res.Unknown)

	if !opts.Apply {
		return res, nil
	}

	for _, item := range res.ToDelete {
		if _, err := s.db.Delete(item.ID); err != nil {
			return res, fmt.Errorf("delete %s: %w", item.ID, err)
		}
	}
	for _, tc := range res.TypeChanges {
		if err := s.db.UpdateType(tc.ID, tc.NewType); err != nil {
			return res, fmt.Errorf("update type %s: %w", tc.ID, err)
		}
	}
	res.Applied = true
	return res, nil
}

// ReformatOneResult reports the outcome of a single-memory LLM refresh.
type ReformatOneResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Rendered bool   `json:"rendered"`
	Skipped  bool   `json:"skipped,omitempty"`
}

// ReformatOne re-runs the LLM render pipeline for exactly one memory and
// writes the result back to the DB + vault. Single-memory scope is
// intentional: bulk reformat is a blast-radius hazard because a single
// invocation can burn large amounts of subscription quota and cascade a bad
// prompt change across the entire knowledge base. Callers wanting to refresh
// several memories must invoke this repeatedly with explicit IDs.
//
// Errors from the LLM propagate to the caller (no silent fallback): the user
// explicitly asked for this render, so if claude failed they need to see why
// rather than get a misleadingly "successful" unchanged file.
func (s *MemoryStore) ReformatOne(id string) (*ReformatOneResult, error) {
	if s.renderer == nil {
		return nil, fmt.Errorf("llm_md_export is not enabled; set enabled: true in config.yaml")
	}
	m, err := s.db.Get(id)
	if err != nil {
		return nil, fmt.Errorf("get memory: %w", err)
	}
	if m == nil {
		return nil, fmt.Errorf("no memory with id %s", id)
	}

	rendered, err := s.renderer.Render(context.Background(), m.Content)
	if err != nil {
		return nil, fmt.Errorf("llm render: %w", err)
	}

	title := vault.Title(m.Content)
	if rendered == m.RenderedBody {
		return &ReformatOneResult{ID: m.ID, Title: title, Skipped: true}, nil
	}
	by := s.renderer.Model()
	if err := s.db.UpdateRenderedBody(m.ID, rendered, by); err != nil {
		return nil, fmt.Errorf("db update: %w", err)
	}
	m.RenderedBody = rendered
	m.RenderedBy = by
	s.syncVault(m)
	return &ReformatOneResult{ID: m.ID, Title: title, Rendered: true}, nil
}

func (s *MemoryStore) FindSimilar(content string) ([]model.MemoryWithScore, error) {
	emb, err := s.embedder.Embed(content)
	if err != nil {
		return nil, err
	}
	return s.db.KNNSearch(emb, 5, nil)
}

// StatusInfo is the aggregated health and inventory report produced by the
// `memo status` command and the `memo_status` MCP tool. Fields are split into
// three categories so consumers can pick what they need:
//   - configuration: Paths, Embedding, LLMRender, Types
//   - inventory:     Memory
//   - health:        Files, Vault, LastRenderError
type StatusInfo struct {
	Paths     StatusPaths     `json:"paths"`
	Memory    StatusMemory    `json:"memory"`
	Files     StatusFiles     `json:"files"`
	Vault     StatusVault     `json:"vault"`
	Embedding StatusEmbedding `json:"embedding"`
	LLMRender StatusLLMRender `json:"llm_render"`
	Types     []StatusType    `json:"types"`

	// LastRenderError is populated when at least one async LLM render has
	// failed since this process started. Single-slot: only the most recent
	// failure is kept. Omitted from JSON when nil.
	LastRenderError *StatusRenderError `json:"last_render_error,omitempty"`
}

type StatusPaths struct {
	Config string `json:"config"`
	DB     string `json:"db"`
	Vault  string `json:"vault"`
	Models string `json:"models"`
}

type StatusMemory struct {
	Total         int            `json:"total"`
	ByType        map[string]int `json:"by_type"`
	OldestCreated string         `json:"oldest_created,omitempty"` // RFC3339
	NewestUpdated string         `json:"newest_updated,omitempty"` // RFC3339
}

type StatusFiles struct {
	DBBytes  int64 `json:"db_bytes"`
	WALBytes int64 `json:"wal_bytes"`
	SHMBytes int64 `json:"shm_bytes"`
	WALMode  bool  `json:"wal_mode"`
}

type StatusVault struct {
	Managed        int `json:"managed"`
	Orphans        int `json:"orphans"`
	TypeMismatches int `json:"type_mismatches"`
}

type StatusEmbedding struct {
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	CacheDir   string `json:"cache_dir"`
}

type StatusLLMRender struct {
	Enabled        bool   `json:"enabled"`
	Command        string `json:"command,omitempty"`
	Model          string `json:"model,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type StatusType struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

type StatusRenderError struct {
	When       time.Time `json:"when"`
	MemoryID   string    `json:"memory_id"`
	MemoryType string    `json:"memory_type,omitempty"`
	Error      string    `json:"error"`
}

// Status builds a snapshot of inventory, configuration, and health. Read-only:
// it runs a few small SELECTs plus a vault dry-run reconcile. Safe to call at
// any time; never mutates state.
func (s *MemoryStore) Status() (*StatusInfo, error) {
	info := &StatusInfo{
		Paths:     s.statusPaths(),
		Embedding: statusEmbedding(s.config),
		LLMRender: statusLLMRender(s.config),
		Types:     statusTypes(s.config),
		Files:     statusFiles(s.config.DBPath),
	}

	total, err := s.db.CountAll()
	if err != nil {
		return nil, fmt.Errorf("count memories: %w", err)
	}
	byType, err := s.db.CountByType()
	if err != nil {
		return nil, fmt.Errorf("count by type: %w", err)
	}
	oldest, newest, err := s.db.CreatedRange()
	if err != nil {
		return nil, fmt.Errorf("date range: %w", err)
	}
	info.Memory = StatusMemory{
		Total:         total,
		ByType:        byType,
		OldestCreated: oldest,
		NewestUpdated: newest,
	}

	// Vault drift: best-effort. A vault outage (iCloud offline, disk error)
	// must not make `memo status` fail - the inventory signal is still
	// useful on its own. The zero-value StatusVault on failure signals
	// "unable to compute" without a separate error field.
	if s.vault != nil {
		if files, err := s.vault.WalkManaged(); err == nil {
			info.Vault.Managed = len(files)
		}
		if rec, err := s.ReconcileVault(ReconcileOptions{Apply: false}); err == nil {
			info.Vault.Orphans = len(rec.Unknown)
			info.Vault.TypeMismatches = len(rec.TypeChanges)
		}
	}

	info.LastRenderError = s.LastRenderError()
	return info, nil
}

func (s *MemoryStore) statusPaths() StatusPaths {
	home, _ := os.UserHomeDir()
	return StatusPaths{
		Config: filepath.Join(home, ".memo", "config.yaml"),
		DB:     s.config.DBPath,
		Vault:  s.config.VaultPath,
		Models: s.config.Embedding.CacheDir,
	}
}

// statusFiles stats the SQLite main DB plus its WAL and SHM sidecar files.
// Missing sidecars are normal between transactions and report zero bytes
// rather than an error. WALMode is hardcoded true because db.Open configures
// it unconditionally at initialization.
func statusFiles(dbPath string) StatusFiles {
	out := StatusFiles{WALMode: true}
	if st, err := os.Stat(dbPath); err == nil {
		out.DBBytes = st.Size()
	}
	if st, err := os.Stat(dbPath + "-wal"); err == nil {
		out.WALBytes = st.Size()
	}
	if st, err := os.Stat(dbPath + "-shm"); err == nil {
		out.SHMBytes = st.Size()
	}
	return out
}

func statusEmbedding(cfg *config.Config) StatusEmbedding {
	return StatusEmbedding{
		Model:      cfg.Embedding.Model,
		Dimensions: cfg.Embedding.Dimensions,
		CacheDir:   cfg.Embedding.CacheDir,
	}
}

func statusLLMRender(cfg *config.Config) StatusLLMRender {
	out := StatusLLMRender{Enabled: cfg.LLMExport.Enabled}
	if out.Enabled {
		out.Command = cfg.LLMExport.Command
		out.Model = cfg.LLMExport.Model
		out.TimeoutSeconds = cfg.LLMExport.TimeoutSeconds
	}
	return out
}

func statusTypes(cfg *config.Config) []StatusType {
	out := make([]StatusType, len(cfg.Types))
	for i, t := range cfg.Types {
		out[i] = StatusType{
			Name:        t.Name,
			Description: t.Description,
			Default:     t.Default,
		}
	}
	return out
}

func (s *MemoryStore) Recall(query string, limit int) (*model.RecallResult, error) {
	if limit <= 0 {
		limit = 5
	}

	memories, err := s.Search(query, limit, nil)
	if err != nil {
		return nil, err
	}

	var context string
	if len(memories) == 0 {
		context = "No relevant memories found."
	} else {
		lines := make([]string, len(memories))
		for i, m := range memories {
			tagsStr := "none"
			if len(m.Tags) > 0 {
				tagsStr = strings.Join(m.Tags, ", ")
			}
			lines[i] = fmt.Sprintf("%d. [%s] %s\n   Tags: %s\n   Score: %.2f",
				i+1, m.Type, m.Content, tagsStr, m.Score)
		}
		context = strings.Join(lines, "\n\n")
	}

	return &model.RecallResult{Context: context, Memories: memories}, nil
}
