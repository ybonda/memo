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
		{"has blank line", strings.Repeat("Long paragraph one. ", 15) + "\n\n" + strings.Repeat("Long paragraph two. ", 15)},
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
