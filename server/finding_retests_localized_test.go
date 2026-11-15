package server

import "testing"

// Regression coverage for English user-facing finding-retest HTTP errors.
// These handlers require a database, so pin their constants directly using the
// shared error assertion. Agent tool descriptions and seed profiles are covered
// separately from these HTTP responses.
func TestFindingRetestErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"notes_too_long", errFindingRetestNotesTooLong},
		{"agent_missing", errFindingRetestAgentMissing},
		{"tool_required", errFindingRetestToolRequired},
		{"service_stopping", errFindingRetestServiceStopping},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
