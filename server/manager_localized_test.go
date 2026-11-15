package server

import "testing"

// Regression tests for English worker-count validation errors in manager.go.
// The shared assertEnglishError helper checks English
// source messages. SetWorkers(n<=0) returns before m.pg.SetSetting, so an empty
// Manager exercises the real path without a database.
// The assertion detects regressions to non-English source messages.
//
// Call graph (see errWorkersPositive): this message reaches users through the settings
// PUT handler and writeErr 400. It has no agent-tool (actool) input path.
// Startup-recovery errors are operator logs and are outside this test's
// worker-count validation scope.
func TestSetWorkersErrorLocalized(t *testing.T) {
	err := (&Manager{}).SetWorkers(0)
	if err == nil {
		t.Fatal("A worker agent count of 0 must be rejected")
	}
	assertEnglishError(t, "set_workers_nonpositive", err.Error())
}
