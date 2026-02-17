package model

import "testing"

func TestGenerateID_Deterministic(t *testing.T) {
	id1 := GenerateID("test content")
	id2 := GenerateID("test content")
	if id1 != id2 {
		t.Errorf("expected deterministic IDs, got %s and %s", id1, id2)
	}
}

func TestGenerateID_DifferentContent(t *testing.T) {
	id1 := GenerateID("content 1")
	id2 := GenerateID("content 2")
	if id1 == id2 {
		t.Errorf("expected different IDs for different content, got %s", id1)
	}
}

func TestGenerateID_KnownValue(t *testing.T) {
	// Verified from actual CLI output
	id := GenerateID("Go uses goroutines for concurrency")
	expected := "31940748-7e88-2ef6-c885-277aa749d23a"
	if id != expected {
		t.Errorf("expected %s, got %s", expected, id)
	}
}

func TestGenerateID_UUIDFormat(t *testing.T) {
	id := GenerateID("anything")
	// UUID format: 8-4-4-4-12 hex chars
	if len(id) != 36 {
		t.Errorf("expected 36 chars, got %d: %s", len(id), id)
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("unexpected UUID format: %s", id)
	}
}
