package server

import "testing"

// Regression tests for English trigger configuration validation errors in triggers.go.
// The shared assertEnglishError helper checks English source messages.

// TestTriggerErrorConstantsLocalized checks all three error constants for English.
func TestTriggerErrorConstantsLocalized(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"no_condition", errTriggerNoCondition},
		{"tool_set_empty", errTriggerToolSetEmpty},
		{"custom_only", errTriggerCustomOnly},
	}
	for _, c := range cases {
		assertEnglishError(t, c.name, c.msg)
	}
}

// TestValidateTriggerLocalized calls the pure validateTrigger function: missing conditions/tools
// return the English constants, and valid input returns an empty string. pgCreateTrigger and
// pgUpdateTrigger expose that result directly to users through writeErr(400, msg).
func TestValidateTriggerLocalized(t *testing.T) {
	if msg := validateTrigger(&triggerReq{}); msg != errTriggerNoCondition {
		t.Fatalf("missing-condition message = %q, want %q", msg, errTriggerNoCondition)
	}
	if msg := validateTrigger(&triggerReq{OnToolCall: true}); msg != errTriggerToolSetEmpty {
		t.Fatalf("missing-tool message = %q, want %q", msg, errTriggerToolSetEmpty)
	}
	if msg := validateTrigger(&triggerReq{OnFinding: true}); msg != "" {
		t.Fatalf("valid-input message = %q, want empty string", msg)
	}
	if msg := validateTrigger(&triggerReq{OnToolCall: true, ToolNames: []string{"nmap"}}); msg != "" {
		t.Fatalf("valid tool-call message = %q, want empty string", msg)
	}
}
