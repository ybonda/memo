package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type TypeDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Default     bool   `yaml:"default,omitempty"`
}

type EmbeddingConfig struct {
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
	CacheDir   string `yaml:"cache_dir"`
}

// LLMExportConfig configures the optional `claude` CLI pass that rewrites
// memory bodies into richer Obsidian markdown at write time. When Enabled is
// false (the default), memo renders via the deterministic Format() pipeline.
// When true, each memo remember / memo update shells out to Command with the
// raw content, caches the result in memories.rendered_body, and uses it at
// render time. Any failure falls back to the deterministic pipeline silently.
type LLMExportConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Command        string `yaml:"command"`
	Model          string `yaml:"model,omitempty"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type Config struct {
	DBPath             string          `yaml:"db_path"`
	VaultPath          string          `yaml:"vault_path"`
	Embedding          EmbeddingConfig `yaml:"embedding"`
	DuplicateThreshold float32         `yaml:"duplicate_threshold"`
	Types              []TypeDef       `yaml:"types"`
	// AutoCaptureContext controls whether `memo remember` shells out to git
	// to populate branch/commit/project on ingest. Defaults to true. Uses a
	// pointer so existing configs without the key still default to on after
	// the backfill below (a plain bool would zero to false instead).
	AutoCaptureContext *bool `yaml:"auto_capture_context,omitempty"`

	// LLMExport is the optional claude-CLI pass that rewrites memory bodies
	// into richer Obsidian markdown. Disabled by default.
	LLMExport LLMExportConfig `yaml:"llm_md_export"`

	// Derived fields (not in YAML)
	TypeRegistry map[string]TypeDef `yaml:"-"`
	DefaultType  string             `yaml:"-"`
}

// CaptureContext reports whether git auto-capture is enabled.
func (c *Config) CaptureContext() bool {
	if c.AutoCaptureContext == nil {
		return true
	}
	return *c.AutoCaptureContext
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	captureOn := true
	return &Config{
		DBPath:    filepath.Join(home, ".memo", "memories.db"),
		VaultPath: filepath.Join(home, ".memo", "vault"),
		Embedding: EmbeddingConfig{
			Model:      "BAAI/bge-small-en-v1.5",
			Dimensions: 384,
			CacheDir:   filepath.Join(home, ".memo", "models"),
		},
		DuplicateThreshold: 0.90,
		AutoCaptureContext: &captureOn,
		LLMExport: LLMExportConfig{
			Enabled: false,
			Command: "claude",
			// Default to Haiku: the render runs async on every write, so
			// latency matters more than the marginal quality edge Sonnet
			// offers on a mechanical markdown-restructuring task. Haiku
			// finishes typical memos (< 20 KB) well inside the 60s timeout;
			// Sonnet routinely exceeds it on incident-sized inputs and the
			// render gets discarded. Override via config.yaml if you want
			// a specific model ID.
			Model: "haiku",
			// 180s gives ~3x headroom over typical Haiku render latency on
			// medium memos (~10-15 KB). The fixed costs dominate: Claude Code
			// CLI cold-start alone is 5-10s per invocation, plus token-by-token
			// output on 3-4K tokens at Haiku's throughput lands around 40-50s.
			// A 60s ceiling leaves essentially no margin and SIGKILLs on jitter.
			TimeoutSeconds: 180,
		},
		Types: []TypeDef{
			{Name: "note", Description: "General observations, ideas, WIP thoughts", Default: true},
			{Name: "incident", Description: "Production incidents, PagerDuty alerts, outages, escalations, bugs, issues"},
			{Name: "ticket", Description: "Jira tickets, DEVOPS-*, APP-*, OPS-*"},
			{Name: "guides", Description: "Guides, documentation, how-tos, best practices, settings, configurations, gotchas"},
			{Name: "architecture", Description: "Architecture decisions, system design patterns"},
		},
	}
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	configDir := filepath.Join(home, ".memo")
	configPath := filepath.Join(configDir, "config.yaml")

	cfg := DefaultConfig()

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Auto-create default config
			if mkErr := os.MkdirAll(configDir, 0o755); mkErr != nil {
				return nil, fmt.Errorf("cannot create config dir: %w", mkErr)
			}
			out, marshalErr := yaml.Marshal(cfg)
			if marshalErr != nil {
				return nil, fmt.Errorf("cannot marshal default config: %w", marshalErr)
			}
			if writeErr := os.WriteFile(configPath, out, 0o644); writeErr != nil {
				return nil, fmt.Errorf("cannot write default config: %w", writeErr)
			}
		} else {
			return nil, fmt.Errorf("cannot read config: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("cannot parse config: %w", err)
		}
	}

	// Expand ~ in paths
	cfg.DBPath = expandPath(cfg.DBPath)
	cfg.VaultPath = expandPath(cfg.VaultPath)
	cfg.Embedding.CacheDir = expandPath(cfg.Embedding.CacheDir)

	// Backfill VaultPath for configs written before this field existed.
	if cfg.VaultPath == "" {
		cfg.VaultPath = filepath.Join(home, ".memo", "vault")
	}

	// Backfill LLMExport defaults for configs predating this feature or with
	// the field present but partial. Command defaults to `claude` so when a
	// user flips Enabled=true, the rest just works.
	if cfg.LLMExport.Command == "" {
		cfg.LLMExport.Command = "claude"
	}
	if cfg.LLMExport.TimeoutSeconds <= 0 {
		cfg.LLMExport.TimeoutSeconds = 60
	}

	// Build type registry
	cfg.TypeRegistry = make(map[string]TypeDef, len(cfg.Types))
	foundDefault := false
	for _, t := range cfg.Types {
		cfg.TypeRegistry[t.Name] = t
		if t.Default {
			cfg.DefaultType = t.Name
			foundDefault = true
		}
	}
	if !foundDefault && len(cfg.Types) > 0 {
		cfg.DefaultType = cfg.Types[0].Name
	}

	return cfg, nil
}

func (c *Config) ValidateType(typeName string) error {
	if _, ok := c.TypeRegistry[typeName]; ok {
		return nil
	}
	valid := make([]string, 0, len(c.Types))
	for _, t := range c.Types {
		valid = append(valid, t.Name)
	}
	return fmt.Errorf("unknown type %q, valid types: %s", typeName, strings.Join(valid, ", "))
}
