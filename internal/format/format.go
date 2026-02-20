package format

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/ybonda/memo/internal/model"
)

var typeColors = map[string]*color.Color{
	"ticket":     color.New(color.FgGreen),
	"postmortem": color.New(color.FgCyan),
	"architecture": color.New(color.FgMagenta),
	"bug":          color.New(color.FgRed),
	"incident":     color.New(color.FgYellow),
}

var (
	defaultTypeColor = color.New(color.Bold)
	scoreColor       = color.New(color.FgYellow)
	dimColor         = color.New(color.Faint)
)

// PrintMemories formats a list of memories as human-readable cards.
func PrintMemories(w io.Writer, memories []model.Memory) {
	if len(memories) == 0 {
		fmt.Fprintln(w, "No memories found.")
		return
	}
	for i, m := range memories {
		printCard(w, m.Type, m.Content, m.Tags, m.UpdatedAt, m.ID, nil)
		if i < len(memories)-1 {
			fmt.Fprintln(w)
		}
	}
}

// PrintMemoriesWithScore formats scored search results as human-readable cards.
func PrintMemoriesWithScore(w io.Writer, memories []model.MemoryWithScore) {
	if len(memories) == 0 {
		fmt.Fprintln(w, "No memories found.")
		return
	}
	for i, m := range memories {
		score := m.Score
		printCard(w, m.Type, m.Content, m.Tags, m.UpdatedAt, m.ID, &score)
		if i < len(memories)-1 {
			fmt.Fprintln(w)
		}
	}
}

// PrintRecallResult formats a recall result with context and scored memories.
func PrintRecallResult(w io.Writer, result model.RecallResult) {
	if len(result.Memories) == 0 {
		fmt.Fprintln(w, "No memories found.")
		return
	}

	fmt.Fprintln(w, "Context:")
	for _, line := range strings.Split(result.Context, "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}

	fmt.Fprintf(w, "\nMemories (%d):\n\n", len(result.Memories))
	for i, m := range result.Memories {
		score := m.Score
		printCard(w, m.Type, m.Content, m.Tags, m.UpdatedAt, m.ID, &score)
		if i < len(result.Memories)-1 {
			fmt.Fprintln(w)
		}
	}
}

func printCard(w io.Writer, typeName, content string, tags []string, updatedAt string, id string, score *float32) {
	tc := typeColors[typeName]
	if tc == nil {
		tc = defaultTypeColor
	}

	// Line 1: [type] (score%) content
	typeLabel := tc.Sprintf("[%s]", typeName)
	if score != nil {
		pct := scoreColor.Sprintf("(%d%%)", int(math.Round(float64(*score)*100)))
		fmt.Fprintf(w, "%s %s %s\n", typeLabel, pct, content)
	} else {
		fmt.Fprintf(w, "%s %s\n", typeLabel, content)
	}

	// Line 2: metadata
	var parts []string
	if len(tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(tags, ", "))
	}
	parts = append(parts, "updated: "+relativeTime(updatedAt))
	parts = append(parts, "id: "+id)

	sep := dimColor.Sprint("  ·  ")
	dimColor.Fprintf(w, "  %s\n", strings.Join(parts, sep))
}

// relativeTime converts an RFC3339 timestamp to a human-readable relative time.
func relativeTime(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		months := int(d.Hours() / 24 / 30)
		if months < 1 {
			months = 1
		}
		if months == 1 {
			return "1mo ago"
		}
		return fmt.Sprintf("%dmo ago", months)
	}
}
