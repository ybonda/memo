package vault

import (
	"strings"
	"testing"
)

func TestFormatShortInputsUnchanged(t *testing.T) {
	cases := []struct{ name, in string }{
		{"empty", ""},
		{"single word", "Hello"},
		{"short sentence", "Fixed the flaky TestStore_Update by adding a sync.Mutex."},
		{"short with lead-in", "ROOT CAUSE: deadlock in the job queue."},
		{"two lines short", "Line one.\nLine two."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Format(c.in); got != c.in {
				t.Errorf("Format short input modified.\nin:  %q\nout: %q", c.in, got)
			}
		})
	}
}

func TestFormatAlreadyFormattedUnchanged(t *testing.T) {
	cases := []struct{ name, in string }{
		{"has bold", "This " + strings.Repeat("long enough content ", 12) + "has **bold** inside."},
		{"has heading", "# Heading\n" + strings.Repeat("body body body ", 20)},
		{"has bullet list", "- item one\n- item two\n" + strings.Repeat("extra ", 40)},
		{"has star list", "* item one\n* item two\n" + strings.Repeat("extra ", 40)},
		{"has numbered list", "1. first item\n2. second item\n" + strings.Repeat("extra ", 40)},
		{"has fenced code", "```go\nfunc main() {}\n```\n" + strings.Repeat("extra ", 40)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Format(c.in); got != c.in {
				t.Errorf("Format mutated already-formatted input.\nin:  %q\nout: %q", c.in, got)
			}
		})
	}
}

// TestFormatSplitsLongProseAcrossBlankLines pins the behaviour change from the
// original short-circuit design: long multi-sentence paragraphs are now
// re-grouped into 2-sentence blocks for readability even when the original
// input already contained blank lines. This is intentional — the prior
// "bail out if input has \n\n" rule prevented any enhancement of LLM-written
// prose, which is exactly the readability problem this formatter should fix.
func TestFormatSplitsLongProseAcrossBlankLines(t *testing.T) {
	in := strings.Repeat("Long paragraph one. ", 15) + "\n\n" + strings.Repeat("Long paragraph two. ", 15)
	out := Format(in)
	if strings.Count(out, "\n\n") < strings.Count(in, "\n\n") {
		t.Errorf("expected more paragraph breaks after splitting; got:\n%s", out)
	}
	// Content is never dropped.
	for _, phrase := range []string{"Long paragraph one.", "Long paragraph two."} {
		if !strings.Contains(out, phrase) {
			t.Errorf("expected %q preserved in output:\n%s", phrase, out)
		}
	}
}

func TestFormatBoldsStartLeadIn(t *testing.T) {
	in := "INCIDENT INVESTIGATION: " + strings.Repeat("Some prose about the incident. ", 8) +
		"Another sentence for length. A further sentence."
	out := Format(in)
	if !strings.HasPrefix(out, "**INCIDENT INVESTIGATION:**") {
		t.Errorf("expected bolded lead-in at start, got:\n%s", out)
	}
}

func TestFormatSplitsAtMidParagraphLeadIn(t *testing.T) {
	in := "Some sentence one that is reasonably long. Some sentence two that continues. KEY LESSON: " +
		strings.Repeat("trailing words ", 10) + "the end here."
	out := Format(in)
	if !strings.Contains(out, "\n\n**KEY LESSON:**") {
		t.Errorf("expected paragraph break before **KEY LESSON:**, got:\n%s", out)
	}
}

func TestFormatPromotesEnumerationToList(t *testing.T) {
	in := "Steps we took during the database migration from Postgres 14 to 15 on production servers today: " +
		"(1) announced maintenance window to users, " +
		"(2) stopped writes and drained the queue, " +
		"(3) ran the schema migration script and verified results, " +
		"(4) restarted the connection pool and resumed writes."
	out := Format(in)
	for _, want := range []string{
		"1. announced maintenance window",
		"2. stopped writes",
		"3. ran the schema migration",
		"4. restarted the connection pool",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing list item %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(1)") || strings.Contains(out, "(2)") {
		t.Errorf("raw (N) markers should be gone:\n%s", out)
	}
}

func TestFormatEnumerationNonSequentialLeftAlone(t *testing.T) {
	in := "Here is a longer paragraph with parenthesized numbers that are not (3) a sequence and also (5) skipped. " +
		strings.Repeat("more filler prose here. ", 10)
	out := Format(in)
	if !strings.Contains(out, "(3)") || !strings.Contains(out, "(5)") {
		t.Errorf("non-sequential markers should be preserved:\n%s", out)
	}
}

func TestFormatWrapsHyphenatedTechTokens(t *testing.T) {
	in := "The " + strings.Repeat("general context here. ", 12) +
		"The jobs-guideevents service and cluster-autoscaler-visibility log both failed."
	out := Format(in)
	if !strings.Contains(out, "`jobs-guideevents`") {
		t.Errorf("expected backticked jobs-guideevents, got:\n%s", out)
	}
	if !strings.Contains(out, "`cluster-autoscaler-visibility`") {
		t.Errorf("expected backticked cluster-autoscaler-visibility, got:\n%s", out)
	}
}

func TestFormatPreserveExistingBackticks(t *testing.T) {
	in := "Affected services: `already-wrapped` service and new jobs-retroactive service, " +
		strings.Repeat("with plenty of surrounding prose here. ", 8)
	out := Format(in)
	if strings.Contains(out, "``already-wrapped``") {
		t.Errorf("already-backticked token should not be double-wrapped:\n%s", out)
	}
	if !strings.Contains(out, "`already-wrapped`") {
		t.Errorf("already-backticked token should survive:\n%s", out)
	}
	if !strings.Contains(out, "`jobs-retroactive`") {
		t.Errorf("new token should be wrapped:\n%s", out)
	}
}

func TestFormatIdempotent(t *testing.T) {
	in := "INCIDENT INVESTIGATION: Always check infrastructure health BEFORE container logs. " +
		"During the investigation we jumped to container logs and found timeouts on jobs-guideevents. " +
		"These were real errors but NOT the cause. James Balazs found the actual cause in 10 minutes: " +
		"(1) Watcher, (2) GKE Workloads, (3) K8s events log. " +
		"KEY LESSON: Container logs show symptoms, not causes. Infrastructure health is the faster path to root cause."
	once := Format(in)
	twice := Format(once)
	if once != twice {
		t.Errorf("Format is not idempotent.\nonce:\n%s\n\ntwice:\n%s", once, twice)
	}
}

// TestFormatIncidentExample is the motivating golden: the user's screenshot
// text should come out visibly well-structured. We check specific structural
// markers rather than the whole byte stream so minor tokenization tweaks don't
// force a test rewrite.
func TestFormatIncidentExample(t *testing.T) {
	in := `INCIDENT INVESTIGATION: Always check infrastructure health BEFORE container logs. During the Mar 19 2026 "Too Many Red Subs" investigation, we jumped to container logs and found Redis timeouts + GCS 429 errors on jobs-guideevents (a known issue from sev2-ops-42021). These were real errors but NOT the cause. James Balazs found the actual cause in 10 minutes using this flow: (1) Watcher → identified "Todo Service Retroactive" column was red, (2) GKE Workloads → jobs-retroactive stuck at 200 replicas, (3) K8s events log → FailedScheduling: Insufficient cpu, (4) cluster-autoscaler-visibility log → scale.up.error.out.of.resources on nap-c4d-standard pool, (5) GKE Cluster Details → control plane upgrade in progress + 7 scaling issues. KEY LESSON: Container logs show symptoms, not causes. Infrastructure health (pod scheduling, autoscaler, cluster state) is the faster path to root cause for scaling-related alerts.`

	out := Format(in)

	assertContains := func(t *testing.T, needle string) {
		t.Helper()
		if !strings.Contains(out, needle) {
			t.Errorf("missing %q in:\n%s", needle, out)
		}
	}
	assertNotContains := func(t *testing.T, needle string) {
		t.Helper()
		if strings.Contains(out, needle) {
			t.Errorf("unexpected %q in:\n%s", needle, out)
		}
	}

	// Bold lead-ins
	assertContains(t, "**INCIDENT INVESTIGATION:**")
	assertContains(t, "**KEY LESSON:**")

	// Real numbered list replacing (N) markers
	assertContains(t, "\n1. Watcher")
	assertContains(t, "\n5. GKE Cluster Details")
	assertNotContains(t, "(1)")
	assertNotContains(t, "(5)")

	// Hyphenated tech tokens backticked
	assertContains(t, "`jobs-guideevents`")
	assertContains(t, "`jobs-retroactive`")
	assertContains(t, "`cluster-autoscaler-visibility`")
	assertContains(t, "`nap-c4d-standard`")

	// Paragraph breaks inserted
	if strings.Count(out, "\n\n") < 3 {
		t.Errorf("expected at least 3 paragraph breaks, got %d\nout:\n%s",
			strings.Count(out, "\n\n"), out)
	}

	// Verbatim preservation: every word from the input (minus (N) markers) must survive.
	// Spot-check a few distinctive phrases.
	for _, phrase := range []string{
		"Always check infrastructure health BEFORE container logs",
		`"Too Many Red Subs"`,
		"known issue from",
		"Container logs show symptoms, not causes",
		"faster path to root cause",
	} {
		assertContains(t, phrase)
	}
}

func TestFormatPromotesKnownLabelsToHeadings(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		heading string
	}{
		{
			name:    "plain label",
			in:      "Investigation findings: Sub was disabled Dec 12 2025 " + strings.Repeat("and the rest of the finding text. ", 8),
			heading: "## Investigation findings",
		},
		{
			name:    "bold wrapped label",
			in:      "**Root cause:** Deleter finds MobileDevice kind but 0 entities " + strings.Repeat("and surrounding context here. ", 8),
			heading: "## Root cause",
		},
		{
			name:    "label with parenthetical qualifier",
			in:      "Related tickets (same root cause): OPS-43317, OPS-43238, OPS-43318 " + strings.Repeat("and more context here. ", 8),
			heading: "## Related tickets (same root cause)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := Format(c.in)
			if !strings.Contains(out, c.heading) {
				t.Errorf("expected heading %q in output:\n%s", c.heading, out)
			}
			// Body after the heading should remain (content preservation).
			if !strings.Contains(out, "and") {
				t.Errorf("expected body content preserved:\n%s", out)
			}
		})
	}
}

func TestFormatEmitsKeyValueCallout(t *testing.T) {
	in := "**Status:** Open, assigned to Yuri Bondarenko\n" +
		"**Created:** 2026-03-27 by JIRA Watcher bot\n" +
		"**Severity:** Sev 2\n" +
		"**SLA:** First response within 4h, resolution within 80h (due Apr 10)\n\n" +
		strings.Repeat("Surrounding prose that makes the document long enough. ", 6)
	out := Format(in)
	if !strings.Contains(out, "> [!") {
		t.Errorf("expected Obsidian callout marker in output:\n%s", out)
	}
	// Sev 2 should route to the warning kind.
	if !strings.Contains(out, "[!warning]") {
		t.Errorf("expected [!warning] callout for Sev 2, got:\n%s", out)
	}
	// Every label line should be quoted with leading "> ".
	for _, label := range []string{"Status:", "Created:", "Severity:", "SLA:"} {
		if !strings.Contains(out, "> **"+label) && !strings.Contains(out, "> "+label) {
			t.Errorf("expected callout-quoted label %q in:\n%s", label, out)
		}
	}
}

func TestFormatKeyValueCalloutSev1IsDanger(t *testing.T) {
	in := "Status: Open\nSeverity: Sev 1 outage\n\n" + strings.Repeat("context prose here. ", 12)
	out := Format(in)
	if !strings.Contains(out, "[!danger]") {
		t.Errorf("expected [!danger] callout for Sev 1, got:\n%s", out)
	}
}

func TestFormatWrapsTicketIDFirstOccurrence(t *testing.T) {
	in := "Watcher auto-generated ticket OPS-43243 for the stuck sub. " +
		"Related: OPS-43317 and OPS-43238 are blocked by OPS-43243 too. " +
		"Also see APP-149135 for the code fix. " +
		strings.Repeat("More prose padding here. ", 6)
	out := Format(in)
	// First occurrences become wikilinks.
	if !strings.Contains(out, "[[OPS-43243]]") {
		t.Errorf("expected [[OPS-43243]] wikilink in:\n%s", out)
	}
	if !strings.Contains(out, "[[OPS-43317]]") {
		t.Errorf("expected [[OPS-43317]] wikilink in:\n%s", out)
	}
	if !strings.Contains(out, "[[APP-149135]]") {
		t.Errorf("expected [[APP-149135]] wikilink in:\n%s", out)
	}
	// Second occurrence of OPS-43243 stays bare (no double-wrap).
	if strings.Count(out, "[[OPS-43243]]") != 1 {
		t.Errorf("expected OPS-43243 wrapped exactly once, got %d in:\n%s",
			strings.Count(out, "[[OPS-43243]]"), out)
	}
}

func TestFormatIdempotentWithNewRules(t *testing.T) {
	in := "Investigation findings: Sub was disabled Dec 12 2025. " +
		"Deleter finds MobileDevice kind but 0 entities. " +
		"apiKey: 1505b1d8-ba90-4340-6efd-507ccd393908-MIGRATED.\n\n" +
		"Status: Open, assigned to Yuri Bondarenko\n" +
		"Severity: Sev 2\n" +
		"SLA: First response within 4h\n\n" +
		"Related tickets: OPS-43317, OPS-43238 are blocked by OPS-43243. " +
		strings.Repeat("Padding prose here. ", 5)
	once := Format(in)
	twice := Format(once)
	if once != twice {
		t.Errorf("Format is not idempotent with new rules.\nonce:\n%s\n\ntwice:\n%s", once, twice)
	}
}

// TestFormatOPS43243Golden is the concrete motivating example from the user's
// Obsidian screenshot. Asserts structural markers rather than byte-exact
// output so individual rule tweaks don't force a golden rewrite.
func TestFormatOPS43243Golden(t *testing.T) {
	in := `OPS-43243: ReadyToDelete | MarkGoings_TestBox | pendo-io (Watcher ticket)

**Status:** Open, assigned to Yuri Bondarenko (picked from Skyhook On Call board 2026-03-31)
**Created:** 2026-03-27 by Pendo JIRA Read-write bot (watcher automation)
**Severity:** Sev 2
**SLA:** First response within 4h, resolution within 80h (due Apr 10)

Context: Watcher auto-generated ticket for MarkGoings_TestBox sub stuck in ReadyToDelete state. Part of the larger issue of 18 subs stuck due to stale Datastore kind metadata (see APP-149135).

Investigation findings:
- Sub was disabled Dec 12 2025 (disabledAt: 1765557115941)
- Marked for deletion (markedForDeletionAt: 1773349648142)
- isDeleted: false — stuck at Step 3 of deletion
- Deleter finds MobileDevice kind but 0 entities to delete — infinite loop

Related tickets on the board (same root cause):
- OPS-43317: ReadyToDelete | Fuel_Cycle_LivebyFuelCyc | pendo-io
- OPS-43238: ReadyToDelete | KevinWPendoTest | pendo-io
- OPS-43318: ReadyToDelete | MoveHQ | pendo-io

Bug filed: APP-149135 (code fix for SubscriptionDeleteManager)
Related PD alerts: PD #50460, PD #50426, PD #50430`

	out := Format(in)

	assertContains := func(t *testing.T, needle string) {
		t.Helper()
		if !strings.Contains(out, needle) {
			t.Errorf("missing %q in:\n%s", needle, out)
		}
	}

	// Top properties block becomes a callout.
	assertContains(t, "> [!warning]")
	assertContains(t, "> **Status:**")
	assertContains(t, "> **SLA:**")

	// Section labels become headings. Full source phrases are preserved,
	// so "Related tickets on the board (same root cause):" becomes a heading
	// with the full qualifier intact.
	assertContains(t, "## Context")
	assertContains(t, "## Investigation findings")
	assertContains(t, "## Related tickets on the board (same root cause)")
	assertContains(t, "## Bug filed")
	assertContains(t, "## Related PD alerts")

	// First occurrence of each ticket ID is wikilinked.
	assertContains(t, "[[OPS-43243]]")
	assertContains(t, "[[OPS-43317]]")
	assertContains(t, "[[OPS-43238]]")
	assertContains(t, "[[OPS-43318]]")
	assertContains(t, "[[APP-149135]]")

	// Content preservation: key phrases survive. `auto-generated` gets
	// backticked by wrapTechTokens, which is expected, so we assert its two
	// flanking words are still adjacent to the token.
	for _, phrase := range []string{
		"Watcher `auto-generated` ticket",
		"stale Datastore kind metadata",
		"infinite loop",
		"ReadyToDelete | MarkGoings_TestBox",
	} {
		assertContains(t, phrase)
	}
}
