package vault

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	shortIDLen = 8
	slugMaxLen = 40
	titleMaxChars = 80
	titleMaxWords = 12
)

// ShortID returns the first 8 hex chars of a memory UUID (the first segment
// before the first "-"). For malformed or short inputs it returns up to the
// first 8 characters of the input as a best-effort fallback.
func ShortID(id string) string {
	if len(id) >= shortIDLen {
		return id[:shortIDLen]
	}
	return id
}

// Slugify converts arbitrary content into a filesystem-safe slug suitable for
// use in a vault filename. It takes the first non-empty line, NFC-normalizes
// unicode, strips non-ASCII letters/digits, lowercases, and collapses runs of
// non-alphanumeric characters into single hyphens. Returns "" when no usable
// characters remain (empty content, emoji-only, etc.).
func Slugify(content string) string {
	first := firstNonEmptyLine(content)
	if first == "" {
		return ""
	}
	first = norm.NFC.String(first)

	var b strings.Builder
	b.Grow(len(first))
	prevHyphen := false
	for _, r := range strings.ToLower(first) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > slugMaxLen {
		slug = strings.TrimRight(slug[:slugMaxLen], "-")
	}
	return slug
}

// Title returns a human-readable title for the memory's frontmatter. Draws
// from the same source as Slugify (first non-empty line) but preserves casing
// and punctuation, capped at 12 words or 80 chars.
func Title(content string) string {
	first := firstNonEmptyLine(content)
	if first == "" {
		return ""
	}
	first = norm.NFC.String(first)

	words := strings.Fields(first)
	if len(words) > titleMaxWords {
		words = words[:titleMaxWords]
	}
	s := strings.Join(words, " ")
	if len(s) > titleMaxChars {
		s = strings.TrimRight(s[:titleMaxChars], " ")
	}
	return s
}

func firstNonEmptyLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
