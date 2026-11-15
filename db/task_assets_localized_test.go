package db

import "testing"

// TestManualTaskScopeSummaryEnglish pins the manual task-scope provenance label.
// It is stored on task_asset_links.source_summary (AddTaskScope / SetTaskAssetSource)
// and rendered verbatim on the task detail sessions/assets tabs. The existing
// task_assets_test.go asserts that the stored value equals this constant through
// its symbol; this assertion also protects the English user-facing text.
func TestManualTaskScopeSummaryEnglish(t *testing.T) {
	const want = "Manually added by the user on the test assets page"
	if manualTaskScopeSummary != want {
		t.Fatalf("manualTaskScopeSummary = %q, want %q", manualTaskScopeSummary, want)
	}
}
