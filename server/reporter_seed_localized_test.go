package server

import "testing"

// TestReporterSeedLabelsLocalized checks the English display labels seeded for reporter.
// reporterAgentName and reporterAgentDescription are rendered by agentDTO in system/agents.
// renderSystem assembles the system prompt from agent.ReporterDefaultPrompt by key;
// reporter selection is deterministic through the report_finding OnToolCall trigger.
// The prompt and reporterToolCallMessage are covered separately.
// Reuse assertEnglishError to check English responses. [[F35]] [[G133]]
func TestReporterSeedLabelsLocalized(t *testing.T) {
	assertEnglishError(t, "reporter_name", reporterAgentName)
	assertEnglishError(t, "reporter_description", reporterAgentDescription)
}
