package server

import "testing"

// Regression coverage for English user-facing finding traffic HTTP errors.
// The shared assertEnglishError helper checks English messages.
// All four handler paths require a database-backed GetFinding call, so the
// constants are checked directly. Shared evidence validation errors, binary
// placeholders, and agent tool descriptions also use English.
func TestFindingTrafficErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"inherited_readonly", errFindingTrafficInheritedReadonly},
		{"select_required", errFindingTrafficSelectRequired},
		{"version_required", errFindingTrafficVersionRequired},
		{"binding_ids_required", errFindingTrafficBindingIDsRequired},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
