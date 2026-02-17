package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/yuri-bondarenko/memo/internal/model"
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

	conn, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
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
	return nil
}

func (d *DB) Insert(mem *model.Memory, embedding []float32) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tagsJSON, _ := json.Marshal(mem.Tags)

	_, err = tx.Exec(
		`INSERT INTO memories (id, content, type, tags, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		mem.ID, mem.Content, mem.Type, string(tagsJSON), mem.CreatedAt, mem.UpdatedAt,
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

	_, err = tx.Exec(
		`UPDATE memories SET content = ?, type = ?, tags = ?, updated_at = ? WHERE id = ?`,
		mem.Content, mem.Type, string(tagsJSON), mem.UpdatedAt, mem.ID,
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
		`SELECT id, content, type, tags, created_at, updated_at FROM memories WHERE id = ?`, id,
	)
	return scanMemory(row)
}

func (d *DB) KNNSearch(embedding []float32, limit int, memType *string) ([]model.MemoryWithScore, error) {
	blob, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, fmt.Errorf("serialize embedding: %w", err)
	}

	var rows *sql.Rows
	if memType != nil {
		rows, err = d.conn.Query(
			`SELECT m.id, m.content, m.type, m.tags, m.created_at, m.updated_at, v.distance
			FROM memories_vec v
			JOIN memory_vectors mv ON mv.vec_rowid = v.rowid
			JOIN memories m ON m.id = mv.memory_id
			WHERE v.embedding MATCH ? AND k = ? AND m.type = ?
			ORDER BY v.distance`,
			blob, limit, *memType,
		)
	} else {
		rows, err = d.conn.Query(
			`SELECT m.id, m.content, m.type, m.tags, m.created_at, m.updated_at, v.distance
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
			id, content, typ, tagsJSON, createdAt, updatedAt string
			distance                                         float64
		)
		if err := rows.Scan(&id, &content, &typ, &tagsJSON, &createdAt, &updatedAt, &distance); err != nil {
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
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			Score:     float32(1.0 - distance),
		})
	}
	return results, rows.Err()
}

func (d *DB) ListAll(limit int, memType *string) ([]model.Memory, error) {
	var rows *sql.Rows
	var err error

	if memType != nil {
		rows, err = d.conn.Query(
			`SELECT id, content, type, tags, created_at, updated_at FROM memories WHERE type = ? ORDER BY updated_at DESC LIMIT ?`,
			*memType, limit,
		)
	} else {
		rows, err = d.conn.Query(
			`SELECT id, content, type, tags, created_at, updated_at FROM memories ORDER BY updated_at DESC LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []model.Memory
	for rows.Next() {
		var id, content, typ, tagsJSON, createdAt, updatedAt string
		if err := rows.Scan(&id, &content, &typ, &tagsJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var tags []string
		json.Unmarshal([]byte(tagsJSON), &tags)
		if tags == nil {
			tags = []string{}
		}

		results = append(results, model.Memory{
			ID:        id,
			Content:   content,
			Type:      typ,
			Tags:      tags,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}
	return results, rows.Err()
}

func scanMemory(row *sql.Row) (*model.Memory, error) {
	var id, content, typ, tagsJSON, createdAt, updatedAt string
	err := row.Scan(&id, &content, &typ, &tagsJSON, &createdAt, &updatedAt)
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
		ID:        id,
		Content:   content,
		Type:      typ,
		Tags:      tags,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}
