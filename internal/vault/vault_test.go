package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ybonda/memo/internal/model"
)

func TestShortID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a1b2c3d4-e5f6-1111-2222-333333333333", "a1b2c3d4"},
		{"abcd", "abcd"},
		{"", ""},
		{"0123456789abcdef", "01234567"},
	}
	for _, c := range cases {
		if got := ShortID(c.in); got != c.want {
			t.Errorf("ShortID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"simple", "Hello World", "hello-world"},
		{"first line only", "First line\nSecond line", "first-line"},
		{"leading blanks", "\n\n  Real content  \n", "real-content"},
		{"punctuation collapsed", "Foo: bar! Baz?? (yes)", "foo-bar-baz-yes"},
		{"unicode stripped", "café ☕ latte", "caf-latte"},
		{"emoji only", "🔥🎉", ""},
		{"empty", "", ""},
		{"whitespace only", "   \n\t\n  ", ""},
		{"trim trailing hyphens", "***end***", "end"},
		{"length cap", strings.Repeat("a", 100), strings.Repeat("a", slugMaxLen)},
		{"length cap trims hyphen", strings.Repeat("a-", 30), strings.TrimRight(strings.Repeat("a-", 30)[:slugMaxLen], "-")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Slugify(c.in); got != c.want {
				t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"A short title.", "A short title."},
		{"", ""},
		{"   \n\nReal.\n", "Real."},
		{"one two three four five six seven eight nine ten eleven twelve thirteen", "one two three four five six seven eight nine ten eleven twelve"},
	}
	for _, c := range cases {
		if got := Title(c.in); got != c.want {
			t.Errorf("Title(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeTagForObsidian(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"already valid kebab", "jobs-silofiles", "jobs-silofiles"},
		{"already valid with underscores", "Digiday_Media", "Digiday_Media"},
		{"nested tag", "project/memo/core", "project/memo/core"},
		{"spaces to hyphens", "Oldest Raw Event Bundle", "Oldest-Raw-Event-Bundle"},
		{"punctuation collapsed", "severity: sev1!", "severity-sev1"},
		{"multiple spaces collapse", "foo   bar", "foo-bar"},
		{"leading/trailing stripped", "  --tag--  ", "tag"},
		{"empty stays empty", "", ""},
		{"unicode preserved", "日本語", "日本語"},
		{"mixed unicode and punct", "日本語: メモ", "日本語-メモ"},
		{"numeric-only", "12345", "12345"},
		{"only punctuation becomes empty", "!!!", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SanitizeTagForObsidian(c.in); got != c.want {
				t.Errorf("SanitizeTagForObsidian(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRenderFrontmatter(t *testing.T) {
	m := &model.Memory{
		ID:        "a1b2c3d4-e5f6-1111-2222-333333333333",
		Content:   "Hello world\nSecond line.",
		Type:      "note",
		Tags:      []string{"db", "sqlite"},
		CreatedAt: "2026-04-19T10:00:00Z",
		UpdatedAt: "2026-04-19T10:05:00Z",
	}
	out, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	want := []string{
		"---\n",
		"id: a1b2c3d4-e5f6-1111-2222-333333333333\n",
		"title: Hello world\n",
		"type: note\n",
		"tags:\n  - db\n  - sqlite\n",
		"created: ",
		"2026-04-19T10:00:00Z",
		"updated: ",
		"2026-04-19T10:05:00Z",
		"---\n\nHello world\nSecond line.\n",
	}
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("Render output missing %q.\nGot:\n%s", w, s)
		}
	}
	if strings.Contains(s, "rendered_by") {
		t.Errorf("rendered_by should be absent when RenderedBy is empty")
	}
}

func TestRenderFrontmatterWithRenderedBy(t *testing.T) {
	m := &model.Memory{
		ID:           "a1b2c3d4-e5f6-1111-2222-333333333333",
		Content:      "Hello world",
		Type:         "note",
		Tags:         []string{},
		RenderedBody: "# Hello world\n\npolished body",
		RenderedBy:   "haiku",
		CreatedAt:    "2026-04-19T10:00:00Z",
		UpdatedAt:    "2026-04-19T10:05:00Z",
	}
	out, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "rendered_by: haiku\n") {
		t.Errorf("expected rendered_by: haiku in output; got:\n%s", s)
	}
	if !strings.Contains(s, "polished body") {
		t.Errorf("expected LLM body to be emitted")
	}
}

func TestRenderContextKeys(t *testing.T) {
	m := &model.Memory{
		ID:      "a1b2c3d4-e5f6-1111-2222-333333333333",
		Content: "Incident context",
		Type:    "incident",
		Tags:    []string{"pendo-io"},
		Context: map[string]string{
			"project":  "pendo-io/pendo-appengine",
			"branch":   "main",
			"commit":   "38162e7",
			"ticket":   "OPS-43243",
			"pr":       "APP-149135",
			"cwd_name": "pendo-appengine",
			// Alphabetised unknown:
			"zoneinfo": "us-east-1",
			// Reserved key (must be ignored):
			"type": "should-be-dropped",
			// Empty value (must be ignored):
			"skipme": "",
		},
		CreatedAt: "2026-04-20T10:00:00Z",
		UpdatedAt: "2026-04-20T10:05:00Z",
	}
	out, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// Known context keys are present in the pinned order.
	idxProject := strings.Index(s, "project:")
	idxBranch := strings.Index(s, "branch:")
	idxCommit := strings.Index(s, "commit:")
	idxTicket := strings.Index(s, "ticket:")
	idxPR := strings.Index(s, "pr:")
	idxCwd := strings.Index(s, "cwd_name:")
	idxZone := strings.Index(s, "zoneinfo:")
	for _, w := range []struct {
		name string
		idx  int
	}{
		{"project", idxProject},
		{"branch", idxBranch},
		{"commit", idxCommit},
		{"ticket", idxTicket},
		{"pr", idxPR},
		{"cwd_name", idxCwd},
		{"zoneinfo", idxZone},
	} {
		if w.idx < 0 {
			t.Errorf("missing key %q in frontmatter:\n%s", w.name, s)
		}
	}
	if idxProject > idxBranch || idxBranch > idxCommit || idxCommit > idxTicket ||
		idxTicket > idxPR || idxPR > idxCwd || idxCwd > idxZone {
		t.Errorf("context key order not preserved:\n%s", s)
	}

	// Reserved key override is silently dropped.
	if strings.Contains(s, "should-be-dropped") {
		t.Errorf("reserved key override leaked into frontmatter:\n%s", s)
	}

	// Empty value is omitted.
	if strings.Contains(s, "skipme:") {
		t.Errorf("empty-value key should not be emitted:\n%s", s)
	}

	// Base type field is still the memory's own type.
	if !strings.Contains(s, "type: incident\n") {
		t.Errorf("expected `type: incident` in frontmatter:\n%s", s)
	}
}

func TestRenderNoContextMatchesOldLayout(t *testing.T) {
	// Regression guard: Context nil/empty produces exactly the same
	// frontmatter keys as before this feature was added.
	m := &model.Memory{
		ID:        "a1b2c3d4-e5f6-1111-2222-333333333333",
		Content:   "Plain note",
		Type:      "note",
		Tags:      []string{"t1"},
		CreatedAt: "2026-04-20T10:00:00Z",
		UpdatedAt: "2026-04-20T10:00:00Z",
	}
	out, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// No stray context-only keys sneak in.
	for _, forbidden := range []string{"project:", "branch:", "commit:", "ticket:", "pr:"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("unexpected context key %q in context-free memory:\n%s", forbidden, s)
		}
	}
}

func TestRenderEmptyTags(t *testing.T) {
	m := &model.Memory{
		ID:        "abcd1234-0000-0000-0000-000000000000",
		Content:   "body",
		Type:      "note",
		Tags:      nil,
		CreatedAt: "2026-04-19T10:00:00Z",
		UpdatedAt: "2026-04-19T10:00:00Z",
	}
	out, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "tags: []") {
		t.Errorf("expected empty tag list rendered as `tags: []`, got:\n%s", out)
	}
}

func TestSyncCreatesFile(t *testing.T) {
	v := newTestVault(t)
	m := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Hello world")

	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(v.Path(), "note", "a1b2c3d4-hello-world.md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected file %s, err: %v", want, err)
	}
}

func TestSyncPreservesSlugOnContentChange(t *testing.T) {
	v := newTestVault(t)
	m := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Original title")

	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(v.Path(), "note", "a1b2c3d4-original-title.md")
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("stat original: %v", err)
	}

	m.Content = "Totally different content"
	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(original); err != nil {
		t.Errorf("filename should be stable across content edits, but original is gone: %v", err)
	}

	drifted := filepath.Join(v.Path(), "note", "a1b2c3d4-totally-different-content.md")
	if _, err := os.Stat(drifted); err == nil {
		t.Errorf("new slug file %s should not exist without --rename", drifted)
	}
}

func TestSyncMovesFileOnTypeChange(t *testing.T) {
	v := newTestVault(t)
	m := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Hello world")

	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(v.Path(), "note", "a1b2c3d4-hello-world.md")
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("stat old: %v", err)
	}

	m.Type = "feedback"
	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old path should be gone after type change, err=%v", err)
	}
	moved := filepath.Join(v.Path(), "feedback", "a1b2c3d4-hello-world.md")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("moved path %s missing: %v", moved, err)
	}
}

func TestDelete(t *testing.T) {
	v := newTestVault(t)
	m := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Hello world")
	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v.Path(), "note", "a1b2c3d4-hello-world.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be gone, err=%v", err)
	}
}

func TestDeleteMissingFileIsNoop(t *testing.T) {
	v := newTestVault(t)
	if err := v.Delete("ffffffff-0000-0000-0000-000000000000"); err != nil {
		t.Errorf("delete of absent memory should be no-op, got: %v", err)
	}
}

func TestExportAllPrunesOrphans(t *testing.T) {
	v := newTestVault(t)
	keep := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Keep me")
	if err := v.Sync(keep); err != nil {
		t.Fatal(err)
	}

	// Manual orphan: a .md with the vault naming shape but unknown short-id
	orphanDir := filepath.Join(v.Path(), "note")
	orphanPath := filepath.Join(orphanDir, "deadbeef-gone.md")
	if err := os.WriteFile(orphanPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	// User-authored file that doesn't match the shape — must survive
	userPath := filepath.Join(orphanDir, "my-personal-notes.md")
	if err := os.WriteFile(userPath, []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := v.ExportAll([]model.Memory{*keep}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Errorf("expected 1 deletion, got %d (events: %+v)", res.Deleted, res.Events)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("orphan should have been pruned")
	}
	if _, err := os.Stat(userPath); err != nil {
		t.Errorf("user file should be untouched: %v", err)
	}
}

func TestExportAllDryRun(t *testing.T) {
	v := newTestVault(t)
	m := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Hello world")

	res, err := v.ExportAll([]model.Memory{*m}, ExportOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Wrote != 1 {
		t.Errorf("expected 1 intended write, got %d", res.Wrote)
	}
	want := filepath.Join(v.Path(), "note", "a1b2c3d4-hello-world.md")
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("dry-run should not have touched disk, file exists: %v", err)
	}
}

func TestExportAllRenameRegeneratesSlug(t *testing.T) {
	v := newTestVault(t)
	m := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Original title")
	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(v.Path(), "note", "a1b2c3d4-original-title.md")
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("stat original: %v", err)
	}

	m.Content = "Totally different content"
	res, err := v.ExportAll([]model.Memory{*m}, ExportOptions{Rename: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Renamed != 1 {
		t.Errorf("expected 1 rename, got %d", res.Renamed)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Errorf("original should be gone after --rename")
	}
	wantNew := filepath.Join(v.Path(), "note", "a1b2c3d4-totally-different-content.md")
	if _, err := os.Stat(wantNew); err != nil {
		t.Errorf("expected new name %s: %v", wantNew, err)
	}
}

func TestExportAllUnchangedCount(t *testing.T) {
	v := newTestVault(t)
	m := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "Hello world")
	if err := v.Sync(m); err != nil {
		t.Fatal(err)
	}
	res, err := v.ExportAll([]model.Memory{*m}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Unchanged != 1 {
		t.Errorf("expected unchanged=1 on re-sync of same memory, got %d (events=%+v)", res.Unchanged, res.Events)
	}
}

func TestWalkManaged(t *testing.T) {
	v := newTestVault(t)

	// Managed files in a type folder
	m1 := mem("a1b2c3d4-0000-0000-0000-000000000000", "note", "First")
	m2 := mem("deadbeef-0000-0000-0000-000000000000", "architecture", "Second")
	if err := v.Sync(m1); err != nil {
		t.Fatal(err)
	}
	if err := v.Sync(m2); err != nil {
		t.Fatal(err)
	}

	// User-authored file with memo-incompatible name: must be ignored
	userFile := filepath.Join(v.Path(), "note", "my-personal-notes.md")
	if err := os.WriteFile(userFile, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Nested file too deep: must be ignored
	nestedDir := filepath.Join(v.Path(), "note", "archive")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nestedDir, "cafebabe-nested.md")
	if err := os.WriteFile(nestedFile, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := v.WalkManaged()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 managed files, got %d: %+v", len(got), got)
	}

	byShort := map[string]ManagedFile{}
	for _, f := range got {
		byShort[f.ShortID] = f
	}
	if byShort["a1b2c3d4"].TypeFolder != "note" {
		t.Errorf("expected note type folder, got %q", byShort["a1b2c3d4"].TypeFolder)
	}
	if byShort["deadbeef"].TypeFolder != "architecture" {
		t.Errorf("expected architecture type folder, got %q", byShort["deadbeef"].TypeFolder)
	}
}

func TestShortIDFromFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a1b2c3d4-foo.md", "a1b2c3d4"},
		{"a1b2c3d4.md", "a1b2c3d4"},
		{"ghijklmn-foo.md", ""}, // non-hex prefix
		{"short.md", ""},        // too short
		{"user-notes.md", ""},   // dash right after 4 chars, not 8
	}
	for _, c := range cases {
		if got := shortIDFromFilename(c.in); got != c.want {
			t.Errorf("shortIDFromFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Helpers

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	v, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	return v
}

func mem(id, typ, content string) *model.Memory {
	return &model.Memory{
		ID:        id,
		Content:   content,
		Type:      typ,
		Tags:      []string{},
		CreatedAt: "2026-04-19T10:00:00Z",
		UpdatedAt: "2026-04-19T10:00:00Z",
	}
}
