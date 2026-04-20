package vault

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ybonda/memo/internal/model"
)

// reservedFrontmatterKeys cannot be supplied via Context because they are
// managed by memo itself and collisions would break the vault schema or the
// Obsidian Properties UI for memories.
var reservedFrontmatterKeys = map[string]bool{
	"id":      true,
	"title":   true,
	"type":    true,
	"tags":    true,
	"created": true,
	"updated": true,
}

// knownContextKeyOrder pins the emission order of well-known context keys so
// diffs stay stable when the same memory is re-rendered. Anything not on this
// list is appended alphabetically after.
var knownContextKeyOrder = []string{
	"project",
	"branch",
	"commit",
	"ticket",
	"pr",
	"related",
	"cwd_name",
}

// Render produces the full .md file contents for a memory: a YAML frontmatter
// block fenced by "---" lines, a blank line, then the content body run through
// Format for structural markdown shaping, with a guaranteed trailing newline.
//
// Frontmatter layout is: id, title, type, tags, then context keys in a
// deterministic order (known keys first, alphabetical unknowns after), then
// created/updated. Reserved keys supplied via Context are silently dropped.
func Render(m *model.Memory) ([]byte, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}

	addScalar := func(key, value string) {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: value},
		)
	}
	addEncoded := func(key string, v any) error {
		valNode := &yaml.Node{}
		if err := valNode.Encode(v); err != nil {
			return err
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			valNode,
		)
		return nil
	}

	addScalar("id", m.ID)
	if title := Title(m.Content); title != "" {
		addScalar("title", title)
	}
	addScalar("type", m.Type)
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	if err := addEncoded("tags", tags); err != nil {
		return nil, fmt.Errorf("encode tags: %w", err)
	}

	// Context keys: known order first, then alphabetical unknowns. Reserved
	// keys and empty values are skipped.
	seen := make(map[string]bool)
	for _, k := range knownContextKeyOrder {
		if reservedFrontmatterKeys[k] {
			continue
		}
		v, ok := m.Context[k]
		if !ok || v == "" {
			continue
		}
		addScalar(k, v)
		seen[k] = true
	}
	var remaining []string
	for k, v := range m.Context {
		if seen[k] || reservedFrontmatterKeys[k] || v == "" {
			continue
		}
		remaining = append(remaining, k)
	}
	sort.Strings(remaining)
	for _, k := range remaining {
		addScalar(k, m.Context[k])
	}

	addScalar("created", m.CreatedAt)
	addScalar("updated", m.UpdatedAt)

	var yamlBuf bytes.Buffer
	enc := yaml.NewEncoder(&yamlBuf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}

	body := Format(m.Content)

	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(yamlBuf.Bytes())
	out.WriteString("---\n\n")
	out.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}
