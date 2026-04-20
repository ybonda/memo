package capture

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseProject(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"github ssh", "git@github.com:ybonda/memo.git", "ybonda/memo"},
		{"github https", "https://github.com/pendo-io/pendo-appengine.git", "pendo-io/pendo-appengine"},
		{"gitlab ssh nested", "git@gitlab.com:group/sub/proj.git", "group/sub/proj"},
		{"http without .git", "http://example.com/team/repo", "team/repo"},
		{"ssh:// scheme", "ssh://git@host.example.com/team/repo.git", "team/repo"},
		{"empty", "", ""},
		{"unknown format", "not a url", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseProject(c.in); got != c.want {
				t.Errorf("parseProject(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestGitEmptyCwd ensures that Git("") is a pure no-op: no shelling out, no
// panic, returns an empty map.
func TestGitEmptyCwd(t *testing.T) {
	got := Git("")
	if len(got) != 0 {
		t.Errorf("expected empty map for empty cwd, got %v", got)
	}
}

// TestGitNonRepoFallsBack verifies that pointing Git at a directory that is
// not a git repo never errors; the cwd_name fallback is populated from the
// directory basename while branch/commit/project stay absent.
func TestGitNonRepoFallsBack(t *testing.T) {
	dir := t.TempDir()
	got := Git(dir)

	if got["cwd_name"] != filepath.Base(dir) {
		t.Errorf("expected cwd_name = %q, got %q", filepath.Base(dir), got["cwd_name"])
	}
	for _, key := range []string{"branch", "commit", "project"} {
		if v, ok := got[key]; ok {
			t.Errorf("expected %s to be absent in non-repo dir, got %q", key, v)
		}
	}
}

// TestGitPopulatesFromRepo runs against a throwaway git repo so we exercise
// the real git binary. Skipped when git is not on PATH.
func TestGitPopulatesFromRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@e.io",
			"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@e.io",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("remote", "add", "origin", "git@github.com:ybonda/memo.git")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "seed.txt")
	run("commit", "-m", "seed")

	got := Git(dir)
	if got["branch"] != "main" {
		t.Errorf("expected branch=main, got %q", got["branch"])
	}
	if len(got["commit"]) < 4 {
		t.Errorf("expected short commit SHA, got %q", got["commit"])
	}
	if got["project"] != "ybonda/memo" {
		t.Errorf("expected project=ybonda/memo, got %q", got["project"])
	}
	if got["cwd_name"] == "" {
		t.Errorf("expected cwd_name populated, got empty")
	}
}
