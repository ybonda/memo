// Package vault renders memo memories as Markdown files in an Obsidian-style
// vault directory. The vault is a one-way projection of the SQLite store:
// every memory becomes a .md file with YAML frontmatter, organized by type
// in subfolders. User edits inside the vault are silently overwritten on the
// next sync — the DB is the sole source of truth.
package vault

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ybonda/memo/internal/model"
)

// Vault is a handle to a vault directory on disk. It holds no open resources
// and is safe to discard; its methods open/close the filesystem as needed.
type Vault struct {
	path string
}

// ExportOptions controls a full ExportAll run.
type ExportOptions struct {
	// Rename forces slug regeneration from current content. Without it, the
	// slug pinned at first export is preserved so filenames stay stable and
	// any wikilinks inside Obsidian remain intact.
	Rename bool
	// DryRun reports the actions that would be taken but does not touch disk.
	DryRun bool
}

// Action classifies a single filesystem change produced by a sync.
type Action string

const (
	ActionWrite     Action = "write"
	ActionUpdate    Action = "update"
	ActionRename    Action = "rename"
	ActionMove      Action = "move"
	ActionDelete    Action = "delete"
	ActionUnchanged Action = "unchanged"
)

// ExportEvent describes one filesystem change (or pending change, in dry-run).
type ExportEvent struct {
	Action Action `json:"action"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path"`
}

// ExportResult summarizes a full ExportAll run.
type ExportResult struct {
	Path      string        `json:"path"`
	DryRun    bool          `json:"dry_run"`
	Wrote     int           `json:"wrote"`
	Updated   int           `json:"updated"`
	Renamed   int           `json:"renamed"`
	Moved     int           `json:"moved"`
	Deleted   int           `json:"deleted"`
	Unchanged int           `json:"unchanged"`
	Events    []ExportEvent `json:"events,omitempty"`
}

// New constructs a Vault rooted at path, creating the directory if missing.
// The path is expected to be pre-expanded (no tilde). An empty path is an
// error.
func New(path string) (*Vault, error) {
	if path == "" {
		return nil, fmt.Errorf("vault path is empty")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create vault dir %s: %w", path, err)
	}
	return &Vault{path: path}, nil
}

// Path returns the vault root directory.
func (v *Vault) Path() string { return v.path }

// Sync writes a single memory's .md file, preserving any frozen slug and
// moving the file between type folders if the memory's type changed. Used by
// the post-write hook inside the store.
func (v *Vault) Sync(m *model.Memory) error {
	_, err := v.syncOne(m, false, false)
	return err
}

// Delete removes the .md file for the given memory id. No error is returned
// if the file was already absent. Used by the post-forget hook.
func (v *Vault) Delete(id string) error {
	path, err := v.findByShortID(ShortID(id))
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// ExportAll rewrites every memory into the vault and prunes any .md file
// whose short-id is not present in the given slice.
func (v *Vault) ExportAll(mems []model.Memory, opts ExportOptions) (*ExportResult, error) {
	res := &ExportResult{Path: v.path, DryRun: opts.DryRun}
	keepIDs := make(map[string]struct{}, len(mems))

	for i := range mems {
		m := &mems[i]
		keepIDs[ShortID(m.ID)] = struct{}{}
		ev, err := v.syncOne(m, opts.Rename, opts.DryRun)
		if err != nil {
			return res, fmt.Errorf("sync %s: %w", m.ID, err)
		}
		if ev != nil {
			res.Events = append(res.Events, *ev)
			countAction(res, ev.Action)
		}
	}

	orphans, err := v.findOrphans(keepIDs)
	if err != nil {
		return res, fmt.Errorf("find orphans: %w", err)
	}
	for _, path := range orphans {
		if !opts.DryRun {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("remove orphan %s: %w", path, err)
			}
		}
		res.Events = append(res.Events, ExportEvent{Action: ActionDelete, Path: path})
		res.Deleted++
	}

	return res, nil
}

// syncOne upserts a single memory and returns the action taken. The
// filename-stability contract is encoded here: if a file already exists for
// the memory's short-id (in any type folder), its slug is preserved unless
// rename=true. If the memory's type changed, the file is relocated.
func (v *Vault) syncOne(m *model.Memory, rename, dryRun bool) (*ExportEvent, error) {
	short := ShortID(m.ID)
	freshName := filename(short, Slugify(m.Content))
	desiredDir := filepath.Join(v.path, m.Type)
	desiredPath := filepath.Join(desiredDir, freshName)

	existingPath, err := v.findByShortID(short)
	if err != nil {
		return nil, err
	}

	data, err := Render(m)
	if err != nil {
		return nil, err
	}

	var (
		action    Action
		writePath string
	)

	switch {
	case existingPath == "":
		action = ActionWrite
		writePath = desiredPath
	case rename:
		writePath = desiredPath
		if filepath.Dir(existingPath) != desiredDir {
			action = ActionMove
		} else if filepath.Base(existingPath) != freshName {
			action = ActionRename
		} else {
			action = ActionUpdate
		}
	default:
		// Preserve existing slug
		existingName := filepath.Base(existingPath)
		if filepath.Dir(existingPath) != desiredDir {
			writePath = filepath.Join(desiredDir, existingName)
			action = ActionMove
		} else {
			writePath = existingPath
			action = ActionUpdate
		}
	}

	// Unchanged optimization: if nothing will move and the file on disk
	// already matches what we'd write, classify as unchanged.
	if action == ActionUpdate && writePath == existingPath {
		if cur, err := os.ReadFile(writePath); err == nil && bytes.Equal(cur, data) {
			return &ExportEvent{Action: ActionUnchanged, ID: m.ID, Path: writePath}, nil
		}
	}

	if dryRun {
		return &ExportEvent{Action: action, ID: m.ID, Path: writePath}, nil
	}

	if err := os.MkdirAll(filepath.Dir(writePath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(writePath), err)
	}
	if err := os.WriteFile(writePath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", writePath, err)
	}
	if existingPath != "" && existingPath != writePath {
		if err := os.Remove(existingPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove old %s: %w", existingPath, err)
		}
	}

	return &ExportEvent{Action: action, ID: m.ID, Path: writePath}, nil
}

// findByShortID searches every type folder for a file whose short-id prefix
// matches. Returns "" if no match. At most one match is expected given the
// 2^32-space of short-ids; the first is returned if somehow more exist.
func (v *Vault) findByShortID(short string) (string, error) {
	entries, err := os.ReadDir(v.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(v.path, e.Name())

		if matches, err := filepath.Glob(filepath.Join(dir, short+"-*.md")); err != nil {
			return "", err
		} else if len(matches) > 0 {
			return matches[0], nil
		}

		bare := filepath.Join(dir, short+".md")
		if _, err := os.Stat(bare); err == nil {
			return bare, nil
		}
	}
	return "", nil
}

// findOrphans walks the vault and returns every .md file whose short-id is
// not in the keep set. Dotfiles and the vault root itself are skipped.
func (v *Vault) findOrphans(keep map[string]struct{}) ([]string, error) {
	var orphans []string
	err := filepath.WalkDir(v.path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != v.path && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		short := shortIDFromFilename(d.Name())
		if short == "" {
			return nil // unrecognized filename — leave user files alone
		}
		if _, ok := keep[short]; !ok {
			orphans = append(orphans, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return orphans, nil
}

// filename composes the desired file basename from a short-id and a slug.
// Empty slug falls back to <short>.md so we never produce "<short>-.md".
func filename(short, slug string) string {
	if slug == "" {
		return short + ".md"
	}
	return short + "-" + slug + ".md"
}

// shortIDFromFilename parses "abcdef12-foo.md" or "abcdef12.md" and returns
// the 8-char hex prefix, or "" if the filename doesn't match the vault shape.
func shortIDFromFilename(name string) string {
	base := strings.TrimSuffix(name, ".md")
	if len(base) < shortIDLen {
		return ""
	}
	prefix := base[:shortIDLen]
	for _, r := range prefix {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ""
		}
	}
	if len(base) == shortIDLen || base[shortIDLen] == '-' {
		return prefix
	}
	return ""
}

func countAction(res *ExportResult, a Action) {
	switch a {
	case ActionWrite:
		res.Wrote++
	case ActionUpdate:
		res.Updated++
	case ActionRename:
		res.Renamed++
	case ActionMove:
		res.Moved++
	case ActionUnchanged:
		res.Unchanged++
	}
}
