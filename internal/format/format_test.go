package format

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/ybonda/memo/internal/model"
)

func init() {
	// Disable colors in tests for deterministic output.
	color.NoColor = true
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		name     string
		offset   time.Duration
		expected string
	}{
		{"just now", 30 * time.Second, "just now"},
		{"minutes", 5 * time.Minute, "5m ago"},
		{"one minute", 1 * time.Minute, "1m ago"},
		{"hours", 2 * time.Hour, "2h ago"},
		{"one hour", 1 * time.Hour, "1h ago"},
		{"days", 3 * 24 * time.Hour, "3d ago"},
		{"one day", 24 * time.Hour, "1d ago"},
		{"months", 60 * 24 * time.Hour, "2mo ago"},
		{"one month", 31 * 24 * time.Hour, "1mo ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := time.Now().Add(-tt.offset).UTC().Format(time.RFC3339)
			got := relativeTime(ts)
			if got != tt.expected {
				t.Errorf("relativeTime(%q) = %q, want %q", ts, got, tt.expected)
			}
		})
	}
}

func TestRelativeTimeInvalid(t *testing.T) {
	got := relativeTime("not-a-date")
	if got != "not-a-date" {
		t.Errorf("expected raw string back, got %q", got)
	}
}

func TestPrintMemoriesEmpty(t *testing.T) {
	var buf bytes.Buffer
	PrintMemories(&buf, []model.Memory{})
	if got := buf.String(); got != "No memories found.\n" {
		t.Errorf("expected empty state message, got %q", got)
	}
}

func TestPrintMemories(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	memories := []model.Memory{
		{
			ID:        "31940748-abcd-1234-5678-9abcdef01234",
			Content:   "Go uses goroutines",
			Type:      "postmortem",
			Tags:      []string{"go", "concurrency"},
			UpdatedAt: now,
		},
		{
			ID:        "428cee7e-1111-2222-3333-444455556666",
			Content:   "Validate input",
			Type:      "ticket",
			Tags:      []string{"security"},
			UpdatedAt: now,
		},
	}

	var buf bytes.Buffer
	PrintMemories(&buf, memories)
	out := buf.String()

	if !strings.Contains(out, "[postmortem]") {
		t.Error("expected [postmortem] type label")
	}
	if !strings.Contains(out, "[ticket]") {
		t.Error("expected [ticket] type label")
	}
	if !strings.Contains(out, "Go uses goroutines") {
		t.Error("expected memory content")
	}
	if !strings.Contains(out, "go, concurrency") {
		t.Error("expected tags")
	}
	if !strings.Contains(out, "id: 31940748-abcd-1234-5678-9abcdef01234") {
		t.Error("expected full ID")
	}
	// Should have blank line between cards
	if !strings.Contains(out, "\n\n") {
		t.Error("expected blank line separator between cards")
	}
}

func TestPrintMemoriesWithScore(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	memories := []model.MemoryWithScore{
		{
			ID:        "31940748-abcd-1234-5678-9abcdef01234",
			Content:   "Go uses goroutines",
			Type:      "postmortem",
			Tags:      []string{"go"},
			Score:     0.95,
			UpdatedAt: now,
		},
	}

	var buf bytes.Buffer
	PrintMemoriesWithScore(&buf, memories)
	out := buf.String()

	if !strings.Contains(out, "(95%)") {
		t.Errorf("expected score percentage, got:\n%s", out)
	}
	if !strings.Contains(out, "[postmortem]") {
		t.Error("expected type label")
	}
}

func TestPrintMemoriesWithScoreEmpty(t *testing.T) {
	var buf bytes.Buffer
	PrintMemoriesWithScore(&buf, []model.MemoryWithScore{})
	if got := buf.String(); got != "No memories found.\n" {
		t.Errorf("expected empty state message, got %q", got)
	}
}

func TestPrintRecallResult(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	result := model.RecallResult{
		Context: "Go uses goroutines for concurrency.",
		Memories: []model.MemoryWithScore{
			{
				ID:        "31940748-abcd-1234-5678-9abcdef01234",
				Content:   "Go uses goroutines",
				Type:      "postmortem",
				Tags:      []string{"go"},
				Score:     0.95,
				UpdatedAt: now,
			},
		},
	}

	var buf bytes.Buffer
	PrintRecallResult(&buf, result)
	out := buf.String()

	if !strings.Contains(out, "Context:") {
		t.Error("expected Context: header")
	}
	if !strings.Contains(out, "Go uses goroutines for concurrency.") {
		t.Error("expected context content")
	}
	if !strings.Contains(out, "Memories (1):") {
		t.Error("expected Memories count header")
	}
	if !strings.Contains(out, "(95%)") {
		t.Error("expected score percentage")
	}
}

func TestPrintRecallResultEmpty(t *testing.T) {
	var buf bytes.Buffer
	PrintRecallResult(&buf, model.RecallResult{})
	if got := buf.String(); got != "No memories found.\n" {
		t.Errorf("expected empty state message, got %q", got)
	}
}

func TestScoreRounding(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	memories := []model.MemoryWithScore{
		{
			ID:        "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			Content:   "test",
			Type:      "postmortem",
			Score:     0.8249,
			UpdatedAt: now,
		},
	}

	var buf bytes.Buffer
	PrintMemoriesWithScore(&buf, memories)
	out := buf.String()

	if !strings.Contains(out, "(82%)") {
		t.Errorf("expected 82%% (rounded down), got:\n%s", out)
	}
}
