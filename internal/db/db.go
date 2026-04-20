package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/ybonda/memo/internal/model"
)

func init() {
	sqlite_vec.Auto()
}

type DB struct {
	conn       *sql.DB
	dimensions int
}

func Open(path string, dimensions int) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("cannot create db dir: %w", err)
		}
	}

	conn, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=wal&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	d := &DB{conn: conn, dimensions: dimensions}
	if err := d.initSchema(); err != nil {
		conn.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'note',
			tags TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS memories_vec USING vec0 (
			embedding float[%d] distance_metric=cosine
		)`, d.dimensions),
		`CREATE TABLE IF NOT EXISTS memory_vectors (
			memory_id TEXT PRIMARY KEY,
			vec_rowid INTEGER NOT NULL,
			FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type)`,
	}
	for _, s := range stmts {
		if _, err := d.conn.Exec(s); err != nil {
			return fmt.Errorf("schema init failed: %w\nSQL: %s", err, s)
		}
	}
	return d.migrateMemoriesColumns()
}

// migrateMemoriesColumns adds new columns to the memories table if they are
// missing. SQLite's ADD COLUMN is not idempotent, so we check PRAGMA
// table_info first. Existing rows get the DEFAULT value; downstream code can
// treat the column as always present.
func (d *DB) migrateMemoriesColumns() error {
	existing, err := d.memoriesColumns()
	if err != nil {
		return err
	}
	type col struct {
		name, ddl string
	}
	for _, c := range []col{
		{"context_json", `ALTER TABLE memories ADD COLUMN context_json TEXT NOT NULL DEFAULT '{}'`},
		{"rendered_body", `ALTER TABLE memories ADD COLUMN rendered_body TEXT NOT NULL DEFAULT ''`},
	} {
		if existing[c.name] {
			continue
		}
		if _, err := d.conn.Exec(c.ddl); err != nil {
			return fmt.Errorf("migrate %s: %w", c.name, err)
		}
	}
	return nil
}

func (d *DB) memoriesColumns() (map[string]bool, error) {
	rows, err := d.conn.Query(`PRAGMA table_info(memories)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typeName string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typeName, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func (d *DB) Insert(mem *model.Memory, embedding []float32) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tagsJSON, _ := json.Marshal(mem.Tags)
	contextJSON := marshalContext(mem.Context)

	_, err = tx.Exec(
		`INSERT INTO memories (id, content, type, tags, context_json, rendered_body, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID, mem.Content, mem.Type, string(tagsJSON), contextJSON, mem.RenderedBody, mem.CreatedAt, mem.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}

	blob, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("serialize embedding: %w", err)
	}

	res, err := tx.Exec(`INSERT INTO memories_vec (embedding) VALUES (?)`, blob)
	if err != nil {
		return fmt.Errorf("insert embedding: %w", err)
	}

	vecRowID, _ := res.LastInsertId()

	_, err = tx.Exec(
		`INSERT INTO memory_vectors (memory_id, vec_rowid) VALUES (?, ?)`,
		mem.ID, vecRowID,
	)
	if err != nil {
		return fmt.Errorf("insert vector mapping: %w", err)
	}

	return tx.Commit()
}

func (d *DB) Update(mem *model.Memory, embedding []float32) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tagsJSON, _ := json.Marshal(mem.Tags)
	contextJSON := marshalContext(mem.Context)

	_, err = tx.Exec(
		`UPDATE memories SET content = ?, type = ?, tags = ?, context_json = ?, rendered_body = ?, updated_at = ? WHERE id = ?`,
		mem.Content, mem.Type, string(tagsJSON), contextJSON, mem.RenderedBody, mem.UpdatedAt, mem.ID,
	)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}

	var vecRowID int64
	err = tx.QueryRow(`SELECT vec_rowid FROM memory_vectors WHERE memory_id = ?`, mem.ID).Scan(&vecRowID)
	if err != nil {
		return fmt.Errorf("find vector rowid: %w", err)
	}

	blob, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("serialize embedding: %w", err)
	}

	_, err = tx.Exec(`UPDATE memories_vec SET embedding = ? WHERE rowid = ?`, blob, vecRowID)
	if err != nil {
		return fmt.Errorf("update embedding: %w", err)
	}

	return tx.Commit()
}

func (d *DB) Delete(id string) (bool, error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var vecRowID int64
	err = tx.QueryRow(`SELECT vec_rowid FROM memory_vectors WHERE memory_id = ?`, id).Scan(&vecRowID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	_, err = tx.Exec(`DELETE FROM memories_vec WHERE rowid = ?`, vecRowID)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(`DELETE FROM memory_vectors WHERE memory_id = ?`, id)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(`DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return false, err
	}

	return true, tx.Commit()
}

func (d *DB) Exists(id string) (bool, error) {
	var count int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ?`, id).Scan(&count)
	return count > 0, err
}

func (d *DB) Get(id string) (*model.Memory, error) {
	row := d.conn.QueryRow(
		`SELECT id, content, type, tags, context_json, rendered_body, created_at, updated_at FROM memories WHERE id = ?`, id,
	)
	return scanMemory(row)
}

// FindByShortID returns the memory whose UUID begins with the given 8-hex
// short-id, or (nil, nil) if none match. Short-id collisions are theoretically
// possible (2^32 space) but practically absent at personal-vault sizes; the
// first match is returned.
func (d *DB) FindByShortID(short string) (*model.Memory, error) {
	row := d.conn.QueryRow(
		`SELECT id, content, type, tags, context_json, rendered_body, created_at, updated_at FROM memories WHERE id LIKE ? LIMIT 1`,
		short+"%",
	)
	return scanMemory(row)
}

// UpdateRenderedBody replaces just the cached LLM-rendered markdown for a
// memory. Content, type, tags, embedding, and timestamps are untouched, so
// this is safe to call from `memo reformat` without bumping updated_at or
// forcing a re-sync of downstream consumers that key off content edits.
func (d *DB) UpdateRenderedBody(id, rendered string) error {
	res, err := d.conn.Exec(
		`UPDATE memories SET rendered_body = ? WHERE id = ?`,
		rendered, id,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no memory with id %s", id)
	}
	return nil
}

// UpdateType changes a memory's type and bumps updated_at. The embedding is
// not regenerated because content is unchanged. Used by reconcile when a file
// has been moved between type folders inside the vault.
func (d *DB) UpdateType(id, newType string) error {
	now := model.NowRFC3339()
	res, err := d.conn.Exec(
		`UPDATE memories SET type = ?, updated_at = ? WHERE id = ?`,
		newType, now, id,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no memory with id %s", id)
	}
	return nil
}

func (d *DB) KNNSearch(embedding []float32, limit int, memType *string) ([]model.MemoryWithScore, error) {
	blob, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, fmt.Errorf("serialize embedding: %w", err)
	}

	var rows *sql.Rows
	if memType != nil {
		rows, err = d.conn.Query(
			`SELECT m.id, m.content, m.type, m.tags, m.context_json, m.rendered_body, m.created_at, m.updated_at, v.distance
			FROM memories_vec v
			JOIN memory_vectors mv ON mv.vec_rowid = v.rowid
			JOIN memories m ON m.id = mv.memory_id
			WHERE v.embedding MATCH ? AND k = ? AND m.type = ?
			ORDER BY v.distance`,
			blob, limit, *memType,
		)
	} else {
		rows, err = d.conn.Query(
			`SELECT m.id, m.content, m.type, m.tags, m.context_json, m.rendered_body, m.created_at, m.updated_at, v.distance
			FROM memories_vec v
			JOIN memory_vectors mv ON mv.vec_rowid = v.rowid
			JOIN memories m ON m.id = mv.memory_id
			WHERE v.embedding MATCH ? AND k = ?
			ORDER BY v.distance`,
			blob, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("knn search: %w", err)
	}
	defer rows.Close()

	var results []model.MemoryWithScore
	for rows.Next() {
		var (
			id, content, typ, tagsJSON, contextJSON, renderedBody, createdAt, updatedAt string
			distance                                                                    float64
		)
		if err := rows.Scan(&id, &content, &typ, &tagsJSON, &contextJSON, &renderedBody, &createdAt, &updatedAt, &distance); err != nil {
			return nil, err
		}
		var tags []string
		json.Unmarshal([]byte(tagsJSON), &tags)
		if tags == nil {
			tags = []string{}
		}

		results = append(results, model.MemoryWithScore{
			ID:        id,
			Content:   content,
			Type:      typ,
			Tags:      tags,
			Context:   unmarshalContext(contextJSON),
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			Score:     float32(1.0 - distance),
		})
		_ = renderedBody // search results never render a cached body
	}
	return results, rows.Err()
}

func (d *DB) ListAll(limit int, memType *string) ([]model.Memory, error) {
	var rows *sql.Rows
	var err error

	if memType != nil {
		rows, err = d.conn.Query(
			`SELECT id, content, type, tags, context_json, rendered_body, created_at, updated_at FROM memories WHERE type = ? ORDER BY updated_at DESC LIMIT ?`,
			*memType, limit,
		)
	} else {
		rows, err = d.conn.Query(
			`SELECT id, content, type, tags, context_json, rendered_body, created_at, updated_at FROM memories ORDER BY updated_at DESC LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []model.Memory
	for rows.Next() {
		var id, content, typ, tagsJSON, contextJSON, renderedBody, createdAt, updatedAt string
		if err := rows.Scan(&id, &content, &typ, &tagsJSON, &contextJSON, &renderedBody, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var tags []string
		json.Unmarshal([]byte(tagsJSON), &tags)
		if tags == nil {
			tags = []string{}
		}

		results = append(results, model.Memory{
			ID:           id,
			Content:      content,
			Type:         typ,
			Tags:         tags,
			Context:      unmarshalContext(contextJSON),
			RenderedBody: renderedBody,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
		})
	}
	return results, rows.Err()
}

func scanMemory(row *sql.Row) (*model.Memory, error) {
	var id, content, typ, tagsJSON, contextJSON, renderedBody, createdAt, updatedAt string
	err := row.Scan(&id, &content, &typ, &tagsJSON, &contextJSON, &renderedBody, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tags []string
	json.Unmarshal([]byte(tagsJSON), &tags)
	if tags == nil {
		tags = []string{}
	}
	return &model.Memory{
		ID:           id,
		Content:      content,
		Type:         typ,
		Tags:         tags,
		Context:      unmarshalContext(contextJSON),
		RenderedBody: renderedBody,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}

// marshalContext returns the JSON encoding of a Context map. A nil or empty
// map is encoded as `{}` so the stored column always has valid JSON.
func marshalContext(ctx map[string]string) string {
	if len(ctx) == 0 {
		return "{}"
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// unmarshalContext returns the parsed Context map, or nil if the stored JSON
// is empty. Nil is the in-memory convention for "no context"; an empty map
// would serialize as `{}` on write and be indistinguishable from "context
// exists but is empty", so we normalize to nil on read.
func unmarshalContext(s string) map[string]string {
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}
