// Package capture gathers ambient context at memory-ingest time: the current
// git branch, commit SHA, and repo identity. All operations are best-effort:
// callers invoke capture.Git and use whatever subset of keys was populated.
// Failures (not a git repo, git not on PATH, timeout) return an empty map
// with no error — capture must never block or fail a `memo remember` call.
package capture

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout bounds each shelled-out git invocation. Keeping it short so
// `memo remember` stays responsive even on a slow or broken repo.
const gitTimeout = 500 * time.Millisecond

// Git returns ambient capture context derived from the given working
// directory. Recognised keys: branch, commit, project, cwd_name. Keys with
// no value are omitted. The function never errors; on any failure it returns
// whatever partial map it managed to assemble (possibly empty).
func Git(cwd string) map[string]string {
	out := make(map[string]string)

	if cwd == "" {
		return out
	}

	if name := filepath.Base(cwd); name != "" && name != "." && name != "/" {
		out["cwd_name"] = name
	}

	if branch := runGit(cwd, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" && branch != "HEAD" {
		out["branch"] = branch
	}
	if commit := runGit(cwd, "rev-parse", "--short", "HEAD"); commit != "" {
		out["commit"] = commit
	}
	if remote := runGit(cwd, "remote", "get-url", "origin"); remote != "" {
		if project := parseProject(remote); project != "" {
			out["project"] = project
		}
	}

	return out
}

// runGit runs a git subcommand from the given cwd with a bounded timeout and
// returns trimmed stdout. Any error (non-zero exit, timeout, missing binary)
// is swallowed and "" is returned so the caller can continue gathering other
// keys.
func runGit(cwd string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseProject normalises a remote URL (SSH or HTTPS) into "<owner>/<repo>".
// Examples:
//
//	git@github.com:ybonda/memo.git  -> ybonda/memo
//	https://github.com/pendo-io/pendo-appengine.git -> pendo-io/pendo-appengine
//	git@gitlab.com:group/sub/proj.git -> group/sub/proj
//
// Returns "" when the input doesn't match any recognised pattern.
func parseProject(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	if remote == "" {
		return ""
	}

	// SSH: git@host:owner/repo
	if strings.HasPrefix(remote, "git@") {
		if idx := strings.Index(remote, ":"); idx > 0 {
			return remote[idx+1:]
		}
		return ""
	}

	// HTTPS / SSH over https: strip the scheme and host.
	for _, scheme := range []string{"https://", "http://", "ssh://"} {
		if strings.HasPrefix(remote, scheme) {
			rest := remote[len(scheme):]
			if slash := strings.Index(rest, "/"); slash > 0 {
				return rest[slash+1:]
			}
			return ""
		}
	}

	return ""
}
