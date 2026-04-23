package model

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

type Memory struct {
	ID      string            `json:"id"`
	Content string            `json:"content"`
	Type    string            `json:"type"`
	Tags    []string          `json:"tags"`
	Context map[string]string `json:"context,omitempty"`
	// RenderedBody is the optional LLM-rewritten markdown cached for vault
	// export. Populated only when Config.LLMExport.Enabled is true and a
	// successful claude-CLI render has run. Never fed into GenerateID or
	// the embedding so dedup semantics stay content-only.
	RenderedBody string `json:"rendered_body,omitempty"`
	// RenderedBy records which model produced RenderedBody (e.g. "haiku",
	// "sonnet"). Empty string means no LLM render has run — the vault will
	// display the deterministic formatter output. Cleared whenever content
	// changes, bumped whenever the async render or `memo reformat` succeeds.
	RenderedBy string `json:"rendered_by,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type MemoryWithScore struct {
	ID        string            `json:"id"`
	Content   string            `json:"content"`
	Type      string            `json:"type"`
	Tags      []string          `json:"tags"`
	Context   map[string]string `json:"context,omitempty"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
	Score     float32           `json:"score"`
}

type StoreResult struct {
	ID            string  `json:"id"`
	ShortID       string  `json:"short_id,omitempty"`
	Status        string  `json:"status"`
	Type          string  `json:"type,omitempty"`
	SizeBytes     int     `json:"size_bytes,omitempty"`
	TagCount      int     `json:"tag_count,omitempty"`
	SimilarMemory *string `json:"similar_memory,omitempty"`
}

type DeleteResult struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

type UpdateResult struct {
	Updated bool    `json:"updated"`
	Memory  *Memory `json:"memory,omitempty"`
}

type RecallResult struct {
	Context  string            `json:"context"`
	Memories []MemoryWithScore `json:"memories"`
}

// GenerateID produces a SHA256-based UUID from content, matching the Rust implementation.
func GenerateID(content string) string {
	h := sha256.Sum256([]byte(content))

	// First 16 bytes formatted as UUID: 8-4-4-4-12 hex chars
	p1 := binary.BigEndian.Uint32(h[0:4])
	p2 := binary.BigEndian.Uint16(h[4:6])
	p3 := binary.BigEndian.Uint16(h[6:8])
	p4 := binary.BigEndian.Uint16(h[8:10])

	// Last 6 bytes (h[10:16]) as a 48-bit value, matching Rust's u64 with 2 zero-padded leading bytes
	var buf [8]byte
	copy(buf[2:], h[10:16])
	p5 := binary.BigEndian.Uint64(buf[:])

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", p1, p2, p3, p4, p5)
}

func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
