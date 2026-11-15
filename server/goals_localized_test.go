package server

import "testing"

// Regression tests for English precondition errors from admitPausedTask.
// The shared assertEnglishError assertion checks English source messages. The
// admitTaskWhen(requirePaused=true) path locks s.concMu and passes through
// s.m.Task, s.engine.IsDeleting, and beginTaskOperation, so these constants
// are checked directly without a database or engine.
//
// The messages appear in single-task writeErr 409 responses and batch
// items[].error responses. The orchestration path uses action="pause",
// so it never reaches the resume validation.
func TestGoalResumeErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"resume_terminal", errGoalResumeTerminal},
		{"resume_not_paused", errGoalResumeNotPaused},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
