package format

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/ybonda/memo/internal/model"
	"github.com/ybonda/memo/internal/store"
)

var typeColors = map[string]*color.Color{
	"ticket":       color.New(color.FgGreen),
	"guides":       color.New(color.FgCyan),
	"architecture": color.New(color.FgMagenta),
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

// PrintStatus renders a memo status snapshot as sectioned output for TTY use.
// Sections are: Paths, Memory, Files on disk, Vault, Embedding, LLM render,
// and (when present) Last render error. JSON consumers should marshal
// *store.StatusInfo directly and skip this function.
func PrintStatus(w io.Writer, info *store.StatusInfo) {
	header := color.New(color.Bold)

	header.Fprintln(w, "memo status")
	fmt.Fprintln(w)

	header.Fprintln(w, "Paths")
	kv(w, "config", info.Paths.Config)
	kv(w, "db", info.Paths.DB)
	kv(w, "vault", info.Paths.Vault)
	kv(w, "models", info.Paths.Models)
	fmt.Fprintln(w)

	header.Fprintln(w, "Memory")
	kv(w, "total", fmt.Sprintf("%d", info.Memory.Total))
	if info.Memory.Total > 0 {
		kv(w, "by type", renderByType(info.Memory.ByType))
		if info.Memory.OldestCreated != "" {
			kv(w, "oldest", relativeTime(info.Memory.OldestCreated))
		}
		if info.Memory.NewestUpdated != "" {
			kv(w, "newest", relativeTime(info.Memory.NewestUpdated))
		}
	}
	fmt.Fprintln(w)

	header.Fprintln(w, "Files on disk")
	kv(w, "memories.db", humanBytes(info.Files.DBBytes))
	walVal := humanBytes(info.Files.WALBytes)
	if info.Files.WALMode {
		walVal += dimColor.Sprint("   (WAL mode active)")
	}
	kv(w, "memories.db-wal", walVal)
	kv(w, "memories.db-shm", humanBytes(info.Files.SHMBytes))
	fmt.Fprintln(w)

	header.Fprintln(w, "Vault")
	kv(w, "managed", fmt.Sprintf("%d files", info.Vault.Managed))
	kv(w, "orphans", fmt.Sprintf("%d", info.Vault.Orphans))
	kv(w, "type mismatches", fmt.Sprintf("%d", info.Vault.TypeMismatches))
	fmt.Fprintln(w)

	header.Fprintln(w, "Embedding")
	kv(w, "model", info.Embedding.Model)
	kv(w, "dimensions", fmt.Sprintf("%d", info.Embedding.Dimensions))
	fmt.Fprintln(w)

	header.Fprintln(w, "LLM render")
	kv(w, "enabled", fmt.Sprintf("%t", info.LLMRender.Enabled))
	if info.LLMRender.Enabled {
		kv(w, "command", info.LLMRender.Command)
		kv(w, "model", info.LLMRender.Model)
		kv(w, "timeout", fmt.Sprintf("%ds", info.LLMRender.TimeoutSeconds))
	}

	if info.LastRenderError != nil {
		fmt.Fprintln(w)
		color.New(color.Bold, color.FgRed).Fprintln(w, "Last render error")
		e := info.LastRenderError
		kv(w, "when", relativeTime(e.When.Format(time.RFC3339)))
		memLabel := shortID(e.MemoryID)
		if e.MemoryType != "" {
			memLabel += " (" + e.MemoryType + ")"
		}
		kv(w, "memory", memLabel)
		kv(w, "error", e.Error)
	}
}

// kv prints a left-padded, dim-colored key followed by its value.
func kv(w io.Writer, k, v string) {
	fmt.Fprintf(w, "  %s  %s\n", dimColor.Sprintf("%-16s", k), v)
}

// renderByType formats a type→count map as a single line with colored
// type badges matching the rest of the card UI.
func renderByType(byType map[string]int) string {
	if len(byType) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(byType))
	for n := range byType {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		tc := typeColors[n]
		if tc == nil {
			tc = defaultTypeColor
		}
		parts = append(parts, tc.Sprintf("[%s]", n)+fmt.Sprintf(" %d", byType[n]))
	}
	return strings.Join(parts, "  ")
}

func shortID(full string) string {
	if len(full) >= 8 {
		return full[:8]
	}
	return full
}

// humanBytes formats a byte count with a binary-prefix unit (KB/MB/GB...).
// Zero renders as "--" so an absent WAL/SHM sidecar reads as "no file" rather
// than "empty file".
func humanBytes(n int64) string {
	const unit = 1024
	if n == 0 {
		return "--"
	}
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
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
