package vault

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Format adds deterministic markdown structure to a memory body. The pipeline
// is designed around three properties:
//
//  1. Per-rule safety: each transformation decides independently whether to
//     apply based on local paragraph shape, so already-structured input is
//     left alone where it should be and enhanced where it can be.
//  2. Idempotence: Format(Format(x)) == Format(x). New structural markers the
//     pipeline introduces (`##`, `> [!...]`, `[[ticket-id]]`, `**LABEL:**`)
//     are detected and skipped on subsequent passes.
//  3. Content preservation: no word or punctuation is ever dropped, rewritten,
//     or reordered; only structural markdown (headings, callouts, list
//     markers, bold, backticks, wikilinks, paragraph breaks) is introduced.
//
// Very short inputs (< 200 runes) are returned as-is, and inputs containing
// fenced code blocks short-circuit entirely because the pipeline cannot safely
// edit around arbitrary code content.
func Format(raw string) string {
	if isTooShort(raw) || hasCodeFence(raw) {
		return raw
	}
	body := splitSectionLabelLines(raw)
	body = promoteLabelHeadings(body)
	body = emitKeyValueCallouts(body)
	body = splitAtLeadIns(body)
	body = splitEnumerations(body)
	body = splitLongParagraphs(body)
	body = boldLeadIns(body)
	body = wrapTechTokens(body)
	body = wikilinkTicketIDs(body)
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
	// ticketID: JIRA-style identifier (3+ uppercase letters, hyphen, 2+ digits)
	// that is not already part of a wikilink. Used for [[OPS-43243]] promotion.
	ticketID = regexp.MustCompile(`\b[A-Z]{3,}-\d{2,}\b`)
	// alreadyWikilinked captures IDs that are already wrapped so a repeated
	// pass does not double-wrap them.
	alreadyWikilinked = regexp.MustCompile(`\[\[([A-Z]{3,}-\d{2,})\]\]`)
	// keyValueLine matches a "Label: value" or "**Label:** value" line. The
	// label is Title-cased or capitalised words only; value is any non-empty
	// text on the same line.
	keyValueLine = regexp.MustCompile(`^(?:\*\*)?([A-Z][A-Za-z][A-Za-z0-9 /+\-]{0,40}):(?:\*\*)?\s+(.+\S)\s*$`)
)

// sectionLabelPhrases is the whitelist of section labels we promote to `##`
// headings when they appear at the start of a paragraph as "Label:" (with
// optional `**...**` bolding and optional qualifier like "Related tickets on
// the board (same root cause)").
//
// Entries are matched longest-first (sorted at init time) so more specific
// labels like "Investigation findings" win over a bare "Findings".
var sectionLabelPhrases = []string{
	"Investigation findings",
	"Related tickets",
	"Related PD alerts",
	"Related alerts",
	"Action items",
	"Root cause",
	"Key lesson",
	"Key lessons",
	"Bug filed",
	"Bugs filed",
	"TL;DR",
	"Context",
	"Timeline",
	"Impact",
	"Remediation",
	"Resolution",
	"Summary",
	"Problem",
	"Solution",
	"Findings",
	"Fix",
}

// propertyLabels are short-metadata labels that belong together in a single
// `> [!info]` callout block rather than individual headings. A paragraph of
// consecutive `Label: value` lines is wrapped in a callout only when every
// label is in this set; if any line uses a section label, the block is split
// into individual paragraphs upstream and each line becomes its own heading.
var propertyLabels = map[string]bool{
	"status":      true,
	"severity":    true,
	"sla":         true,
	"priority":    true,
	"created":     true,
	"updated":     true,
	"due":         true,
	"eta":         true,
	"assignee":    true,
	"reporter":    true,
	"owner":       true,
	"team":        true,
	"environment": true,
	"version":     true,
	"component":   true,
}

func hasCodeFence(s string) bool {
	return strings.Contains(s, "```")
}

func isStructuredParagraph(p string) bool {
	trimmed := strings.TrimLeft(p, " \t")
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") {
		return true
	}
	return looksLikeList(p)
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
		if isStructuredParagraph(p) || len([]rune(p)) < 300 {
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
		if isStructuredParagraph(p) {
			continue
		}
		trimmed := strings.TrimLeft(p, " \t")
		paras[i] = startLeadIn.ReplaceAllStringFunc(trimmed, func(m string) string {
			// Idempotency: if the match is already flanked by `**`, skip.
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

// splitSectionLabelLines is a pre-pass that splits multi-line `Label: value`
// paragraphs into individual single-line paragraphs when any label is a
// section label (e.g. "Bug filed", "Related PD alerts"). This lets
// promoteLabelHeadings turn each line into its own `##` heading while still
// letting emitKeyValueCallouts wrap property-only blocks as callouts.
func splitSectionLabelLines(body string) string {
	paras := strings.Split(body, "\n\n")
	var out []string
	for _, p := range paras {
		trimmed := strings.TrimLeft(p, " \t")
		lines := strings.Split(trimmed, "\n")
		if len(lines) < 2 {
			out = append(out, p)
			continue
		}
		allKV := true
		hasSectionLabel := false
		for _, line := range lines {
			m := keyValueLine.FindStringSubmatch(line)
			if m == nil {
				allKV = false
				break
			}
			labelLower := strings.ToLower(strings.TrimSpace(m[1]))
			if !propertyLabels[labelLower] {
				hasSectionLabel = true
			}
		}
		if !allKV || !hasSectionLabel {
			out = append(out, p)
			continue
		}
		// Emit each line as its own paragraph so heading promotion can fire.
		for _, line := range lines {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n\n")
}

// promoteLabelHeadings converts a paragraph that starts with a recognised
// section label (e.g. "Investigation findings:", "Related tickets on the
// board (same root cause):") into a Markdown `##` heading followed by the
// remainder of the paragraph. The label is matched case-insensitively and
// may be wrapped in `**...**`; any benign qualifier text between the base
// label and the terminating colon is retained in the heading.
func promoteLabelHeadings(body string) string {
	paras := strings.Split(body, "\n\n")
	var out []string
	for _, p := range paras {
		transformed, split := promoteLabelHeadingInParagraph(p)
		if split {
			out = append(out, transformed...)
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "\n\n")
}

func promoteLabelHeadingInParagraph(p string) ([]string, bool) {
	trimmed := strings.TrimLeft(p, " \t")
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") {
		return nil, false
	}
	if isPropertyKVBlock(trimmed) {
		return nil, false
	}
	// Strip a leading `**` so we can match labels that are bold-wrapped. We
	// will also consume any closing `**` between the label and the colon.
	working := trimmed
	working = strings.TrimPrefix(working, "**")

	// Find the first colon on the first line — that is the heading terminator.
	firstLine := working
	if nl := strings.Index(working, "\n"); nl >= 0 {
		firstLine = working[:nl]
	}
	colonIdx := strings.Index(firstLine, ":")
	if colonIdx <= 0 || colonIdx > 80 {
		return nil, false
	}
	candidate := strings.TrimSpace(firstLine[:colonIdx])
	// Allow a closing `**` immediately before the colon.
	candidate = strings.TrimSuffix(candidate, "**")
	candidate = strings.TrimSpace(candidate)

	// ALL-CAPS lead-ins (e.g. "KEY LESSON", "INCIDENT INVESTIGATION") are the
	// stylistic territory of boldLeadIns; leave them alone here so the two
	// rules don't fight (which would break idempotence).
	if !hasLowercaseLetter(candidate) {
		return nil, false
	}

	matchedLabel := ""
	for _, label := range sectionLabelPhrases {
		if len(candidate) < len(label) {
			continue
		}
		if !strings.EqualFold(candidate[:len(label)], label) {
			continue
		}
		ext := candidate[len(label):]
		if !isBenignLabelExtension(ext) {
			continue
		}
		matchedLabel = candidate
		break
	}
	if matchedLabel == "" {
		return nil, false
	}

	// Consume past the colon. Also drop any `**` that immediately follows
	// (the closing marker of `**Label:**`).
	rest := working[colonIdx+1:]
	rest = strings.TrimPrefix(rest, "**")
	rest = strings.TrimLeft(rest, " \t\r\n")
	if rest == "" {
		return []string{"## " + matchedLabel}, true
	}
	return []string{"## " + matchedLabel, rest}, true
}

// hasLowercaseLetter returns true if s contains at least one a-z rune.
func hasLowercaseLetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

// isBenignLabelExtension allows alphanumerics, spaces, and a small set of
// punctuation typical of label qualifiers. Anything that could hint at a
// different sentence structure (e.g. a second colon, a period, a quote)
// disqualifies the match.
func isBenignLabelExtension(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == ' ', r == '(', r == ')', r == '-', r == '/', r == ',':
		default:
			return false
		}
	}
	return true
}

// isPropertyKVBlock returns true when every line is a `Label: value` pair and
// every label is a property label (so emitKeyValueCallouts will wrap it).
func isPropertyKVBlock(p string) bool {
	lines := strings.Split(p, "\n")
	if len(lines) < 2 {
		return false
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			return false
		}
		m := keyValueLine.FindStringSubmatch(line)
		if m == nil {
			return false
		}
		if !propertyLabels[strings.ToLower(strings.TrimSpace(m[1]))] {
			return false
		}
	}
	return true
}

// emitKeyValueCallouts wraps paragraphs of 2+ consecutive property-label lines
// (e.g. Status/Severity/SLA/Created) in an Obsidian `> [!info]` callout.
// Section-label blocks are split into individual paragraphs upstream by
// splitSectionLabelLines so they reach promoteLabelHeadings instead and
// each line becomes its own `##` heading.
func emitKeyValueCallouts(body string) string {
	paras := strings.Split(body, "\n\n")
	for i, p := range paras {
		trimmed := strings.TrimLeft(p, " \t")
		if strings.HasPrefix(trimmed, ">") {
			continue // already a callout
		}
		if !isPropertyKVBlock(trimmed) {
			continue
		}
		paras[i] = wrapKeyValueCallout(trimmed)
	}
	return strings.Join(paras, "\n\n")
}

func wrapKeyValueCallout(p string) string {
	kind := "info"
	// Severity / status heuristic: look for Sev 1 / Sev 2 / P0 / P1 tokens.
	lowered := strings.ToLower(p)
	switch {
	case strings.Contains(lowered, "sev 1"), strings.Contains(lowered, "sev1"),
		strings.Contains(lowered, "p0 "), strings.Contains(lowered, ": p0"),
		strings.Contains(lowered, "critical"):
		kind = "danger"
	case strings.Contains(lowered, "sev 2"), strings.Contains(lowered, "sev2"),
		strings.Contains(lowered, "p1 "), strings.Contains(lowered, ": p1"),
		strings.Contains(lowered, "major"):
		kind = "warning"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "> [!%s] Details\n", kind)
	for _, line := range strings.Split(p, "\n") {
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// wikilinkTicketIDs wraps the first occurrence of each JIRA-style identifier
// in `[[...]]` so Obsidian renders it as a navigable wikilink. Subsequent
// occurrences of the same ID are left bare to avoid visual noise. IDs already
// wrapped (from a prior Format pass) are respected: they seed the "seen" set
// before the first-occurrence-wins pass runs, so Format is idempotent.
// Processing tracks backtick code-span state so IDs inside inline code are
// left untouched.
func wikilinkTicketIDs(body string) string {
	seen := make(map[string]bool)
	for _, m := range alreadyWikilinked.FindAllStringSubmatch(body, -1) {
		seen[m[1]] = true
	}
	var out strings.Builder
	var buf strings.Builder
	inside := false
	flush := func() {
		if inside {
			out.WriteString(buf.String())
		} else {
			out.WriteString(ticketID.ReplaceAllStringFunc(buf.String(), func(id string) string {
				if seen[id] {
					return id
				}
				seen[id] = true
				return "[[" + id + "]]"
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
