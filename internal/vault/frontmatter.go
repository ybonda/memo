package vault

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/ybonda/memo/internal/model"
)

// frontmatter is the YAML block prepended to every exported memory file. The
// field set and order is intentionally stable: Obsidian's Properties UI types
// each field by first-seen value, so changing the shape after users have
// opened the vault once can require manual reconciliation.
type frontmatter struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title,omitempty"`
	Type    string   `yaml:"type"`
	Tags    []string `yaml:"tags"`
	Created string   `yaml:"created"`
	Updated string   `yaml:"updated"`
}

// Render produces the full .md file contents for a memory: a YAML frontmatter
// block fenced by "---" lines, a blank line, then the raw content body with a
// guaranteed trailing newline.
func Render(m *model.Memory) ([]byte, error) {
	fm := frontmatter{
		ID:      m.ID,
		Title:   Title(m.Content),
		Type:    m.Type,
		Tags:    m.Tags,
		Created: m.CreatedAt,
		Updated: m.UpdatedAt,
	}
	if fm.Tags == nil {
		fm.Tags = []string{}
	}

	var yamlBuf bytes.Buffer
	enc := yaml.NewEncoder(&yamlBuf)
	enc.SetIndent(2)
	if err := enc.Encode(&fm); err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}

	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(yamlBuf.Bytes())
	out.WriteString("---\n\n")
	out.WriteString(m.Content)
	if !bytes.HasSuffix([]byte(m.Content), []byte("\n")) {
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}
