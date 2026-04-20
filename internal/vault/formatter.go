package vault

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Format adds deterministic markdown structure to a memory body: bolds
// ALL-CAPS lead-in labels ending in a colon, promotes inline "(1)(2)(3)"
// enumerations to numbered lists, splits long multi-sentence paragraphs, and
// wraps hyphenated-lowercase technical identifiers in backticks. The function
// never adds, removes, or reorders content; only structural whitespace,
// punctuation-equivalent emphasis (** and `) and list markers are introduced.
//
// Short inputs (< 200 runes or fewer than 2 sentences) and inputs that already
// contain structural markdown (blank lines, headings, lists, bold, or fenced
// code) are returned unchanged, so hand-written markdown passes through
// byte-for-byte.
func Format(raw string) string {
	if alreadyFormatted(raw) || isTooShort(raw) {
		return raw
	}
	body := splitAtLeadIns(raw)
	body = splitEnumerations(body)
	body = splitLongParagraphs(body)
	body = boldLeadIns(body)
	body = wrapTechTokens(body)
	return body
}

var (
	// midParaLeadIn: sentence-terminator + whitespace followed by an ALL-CAPS
	// lead-in label of at least 3 letters ending in ":". Used to inject
	// paragraph breaks so each lead-in starts its own paragraph.
	midParaLeadIn = regexp.MustCompile(`([.!?])\s+([A-Z][A-Z][A-Z \-]{1,40}:)`)
	// startLeadIn: ALL-CAPS label anchored to string start, for bolding.
	startLeadIn = regexp.MustCompile(`^[A-Z][A-Z][A-Z \-]{1,40}:`)
	// numMarker: "(N) " enumeration marker.
	numMarker = regexp.MustCompile(`\((\d+)\)\s+`)
	// techToken: two or more lowercase alphanumeric segments joined by
	// hyphens (jobs-guideevents, cluster-autoscaler-visibility, sev2-ops-42021).
	techToken = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:-[a-z0-9]+)+\b`)
	// sentenceEnd: ".", "!", or "?" followed by whitespace.
	sentenceEnd = regexp.MustCompile(`[.!?]\s+`)
)

func alreadyFormatted(s string) bool {
	if strings.Contains(s, "\n\n") || strings.Contains(s, "**") || strings.Contains(s, "```") {
		return true
	}
	for line := range strings.SplitSeq(s, "\n") {
		t := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(t, "# ") || strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "### ") ||
			strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			return true
		}
		if isNumberedListPrefix(t) {
			return true
		}
	}
	return false
}

func isNumberedListPrefix(t string) bool {
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(t) && t[i] == '.' && t[i+1] == ' '
}

func isTooShort(s string) bool {
	return len([]rune(s)) < 200
}

func splitAtLeadIns(s string) string {
	return midParaLeadIn.ReplaceAllString(s, "$1\n\n$2")
}

func splitEnumerations(body string) string {
	paras := strings.Split(body, "\n\n")
	for i, p := range paras {
		paras[i] = splitEnumerationsInParagraph(p)
	}
	return strings.Join(paras, "\n\n")
}

func splitEnumerationsInParagraph(p string) string {
	matches := numMarker.FindAllStringSubmatchIndex(p, -1)
	if len(matches) < 2 {
		return p
	}
	for i, m := range matches {
		n, err := strconv.Atoi(p[m[2]:m[3]])
		if err != nil || n != i+1 {
			return p
		}
	}

	first := matches[0]
	pre := strings.TrimRight(p[:first[0]], " \t,;")

	items := make([]string, len(matches))
	for i, m := range matches {
		start := m[1]
		end := len(p)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		item := strings.TrimSpace(p[start:end])
		items[i] = strings.TrimRight(item, ",;.")
	}

	var b strings.Builder
	if pre != "" {
		b.WriteString(pre)
		if !strings.HasSuffix(pre, ":") {
			b.WriteString(":")
		}
		b.WriteString("\n\n")
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	return strings.TrimRight(b.String(), "\n")
}

func splitLongParagraphs(body string) string {
	paras := strings.Split(body, "\n\n")
	var out []string
	for _, p := range paras {
		if looksLikeList(p) || len([]rune(p)) < 300 {
			out = append(out, p)
			continue
		}
		sentences := splitSentences(p)
		if len(sentences) < 3 {
			out = append(out, p)
			continue
		}
		out = append(out, groupSentences(sentences)...)
	}
	return strings.Join(out, "\n\n")
}

func looksLikeList(p string) bool {
	for line := range strings.SplitSeq(p, "\n") {
		t := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || isNumberedListPrefix(t) {
			return true
		}
	}
	return false
}

func splitSentences(p string) []string {
	var sentences []string
	start := 0
	for {
		loc := sentenceEnd.FindStringIndex(p[start:])
		if loc == nil {
			tail := strings.TrimSpace(p[start:])
			if tail != "" {
				sentences = append(sentences, tail)
			}
			break
		}
		absPunct := start + loc[0]
		absNext := start + loc[1]
		candidate := p[start : absPunct+1]
		if isAbbreviation(candidate) {
			start = absNext
			continue
		}
		sentences = append(sentences, strings.TrimSpace(candidate))
		start = absNext
	}
	return sentences
}

func isAbbreviation(s string) bool {
	if s == "" || s[len(s)-1] != '.' {
		return false
	}
	j := len(s) - 1
	for j > 0 {
		c := s[j-1]
		if c == ' ' || c == '\t' || c == '(' {
			break
		}
		j--
	}
	word := strings.ToLower(s[j : len(s)-1])
	switch word {
	case "e.g", "i.e", "vs", "dr", "mr", "mrs", "ms", "st", "jr", "sr":
		return true
	}
	if len(word) == 1 {
		return true
	}
	if len(word) >= 2 && word[0] == 'v' && word[1] >= '0' && word[1] <= '9' {
		return true
	}
	return false
}

func groupSentences(sentences []string) []string {
	var out []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, s := range sentences {
		cur = append(cur, s)
		trimmed := strings.TrimSpace(s)
		if startLeadIn.MatchString(trimmed) ||
			strings.HasSuffix(trimmed, ":") ||
			len(cur) >= 2 {
			flush()
		}
	}
	flush()
	return out
}

func boldLeadIns(body string) string {
	paras := strings.Split(body, "\n\n")
	for i, p := range paras {
		trimmed := strings.TrimLeft(p, " \t")
		paras[i] = startLeadIn.ReplaceAllStringFunc(trimmed, func(m string) string {
			return "**" + m + "**"
		})
	}
	return strings.Join(paras, "\n\n")
}

func wrapTechTokens(body string) string {
	var out strings.Builder
	var buf strings.Builder
	inside := false
	flush := func() {
		if inside {
			out.WriteString(buf.String())
		} else {
			out.WriteString(techToken.ReplaceAllStringFunc(buf.String(), func(tok string) string {
				return "`" + tok + "`"
			}))
		}
		buf.Reset()
	}
	for _, r := range body {
		if r == '`' {
			flush()
			inside = !inside
			out.WriteRune(r)
			continue
		}
		buf.WriteRune(r)
	}
	flush()
	return out.String()
}
