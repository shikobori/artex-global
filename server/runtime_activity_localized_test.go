package server

import (
	"fmt"
	"strings"
	"testing"
)

// Regression tests for English runtime activity summaries from engine.go,
// engine_timeout.go, and goals.go. These task-level transcript entries have no
// node_id; intent-scoped result/text readers do not feed them back to agents.
// Reuses the shared error-language assertion.
func TestRuntimeActivitySummariesLocalized(t *testing.T) {
	// Fixed summaries without format arguments must be English.
	plain := map[string]string{
		"goalless_task_done": goallessTaskDoneSummary,
		"goal_breakdown_r0":  goalBreakdownRound0Summary,
		"queued_no_llm":      queuedNoLLMSummary,
		"queued_fifo":        queuedFIFOSummary,
	}
	for label, msg := range plain {
		assertEnglishError(t, label, msg)
	}

	// Check that format verbs survive and substituted numbers appear in the result.
	fmtCases := map[string]string{
		"planner_round":       plannerRoundSummaryFmt,
		"timeout_final_round": timeoutFinalRoundSummaryFmt,
		"queued_conc_limit":   queuedConcurrencyLimitSummaryFmt,
	}
	for label, f := range fmtCases {
		if !strings.Contains(f, "%d") {
			t.Fatalf("%s: format verb %%d is missing: %q", label, f)
		}
		got := fmt.Sprintf(f, 7)
		assertEnglishError(t, label, got)
		if !strings.Contains(got, "7") {
			t.Fatalf("%s: format argument is missing from the result: %q", label, got)
		}
	}
}
