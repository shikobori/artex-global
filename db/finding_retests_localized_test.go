package db

import (
	"testing"
	"unicode"
)

// assertRetestReasonEnglish checks that user-visible retest reasons are nonempty English text.
// These constants are embedded in FinishFindingRetest and RecoverFindingRetests SQL,
// so checking them also protects the text shown in the retest panel.
func assertRetestReasonEnglish(t *testing.T, name, s string) {
	t.Helper()
	if s == "" {
		t.Fatalf("%s: empty string", name)
	}
	hasLatin := false
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) {
			t.Fatalf("%s: non-English characters remain: %q", name, s)
		}
		if unicode.Is(unicode.Latin, r) {
			hasLatin = true
		}
	}
	if !hasLatin {
		t.Fatalf("%s: no English letters: %q", name, s)
	}
}

// TestFindingRetestReasonsLocalized checks the terminal reasons stored in finding_retests.error
// and shown as item.error in the retest panel. They share a column and panel with convRetest*
// reasons in server/conversations.go, so both must use English consistently.
func TestFindingRetestReasonsLocalized(t *testing.T) {
	assertRetestReasonEnglish(t, "retestNoConclusionReason", retestNoConclusionReason)
	assertRetestReasonEnglish(t, "retestServiceRestartReason", retestServiceRestartReason)
}
