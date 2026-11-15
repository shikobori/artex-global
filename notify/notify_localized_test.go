package notify

import (
	"strings"
	"testing"
)

// hasHan reports whether text contains a CJK unified ideograph, catching label language regressions.
func hasHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// hasHangul reports whether text contains a Hangul syllable.
func hasHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

// assertEnglish verifies that English default labels contain no Han or
// Hangul characters.
func assertEnglish(t *testing.T, where, got string) {
	t.Helper()
	if hasHan(got) {
		t.Errorf("%s: Han characters remain: %q", where, got)
	}
	if hasHangul(got) {
		t.Errorf("%s: Hangul characters remain: %q", where, got)
	}
}

// TestSeverityLabelLocalized checks English severity labels shared by all notification channels.
func TestSeverityLabelLocalized(t *testing.T) {
	// Match English severity labels used by the UI.
	want := map[string]string{
		"critical": "Critical",
		"high":     "High",
		"medium":   "Medium",
		"low":      "Low",
	}
	for sev, label := range want {
		got := SeverityLabel(sev)
		assertEnglish(t, "SeverityLabel("+sev+")", got)
		if !strings.Contains(got, label) {
			t.Errorf("SeverityLabel(%q)=%q, must contain %q", sev, got, label)
		}
	}
	// Unknown severity values are returned verbatim rather than inventing a label.
	if got := SeverityLabel("made_up"); got != "made_up" {
		t.Errorf("Unknown severity must remain unchanged, got %q", got)
	}
}

// TestStatusLabelLocalized checks all nine English finding status labels match the UI.
func TestStatusLabelLocalized(t *testing.T) {
	want := map[string]string{
		"pending":        "Pending",
		"in_progress":    "In progress",
		"confirmed":      "Confirmed",
		"resolved":       "Resolved",
		"fixed":          "Fixed",
		"false_positive": "False positive",
		"ignored":        "Ignored",
		"duplicate":      "Duplicate",
		"risk_accepted":  "Risk accepted",
	}
	for status, label := range want {
		got := StatusLabel(status)
		assertEnglish(t, "StatusLabel("+status+")", got)
		if got != label {
			t.Errorf("StatusLabel(%q)=%q, expected %q", status, got, label)
		}
	}
	// Return unknown statuses verbatim rather than inventing a label.
	if got := StatusLabel("weird_status"); got != "weird_status" {
		t.Errorf("Unknown status must remain unchanged, got %q", got)
	}
}

// TestItemTitlePlaceholderLocalized checks the English fallback when both name and class are empty;
// titles must never be blank.
func TestItemTitlePlaceholderLocalized(t *testing.T) {
	got := Item{}.Title()
	assertEnglish(t, "Item{}.Title()", got)
	// Prefer a supplied name, then a class; preserve user-provided Unicode content.
	if n := (Item{Name: "로그인 SQLi"}).Title(); n != "로그인 SQLi" {
		t.Errorf("Name precedence failed, got %q", n)
	}
	if v := (Item{VulnClass: "XSS"}).Title(); v != "XSS" {
		t.Errorf("Class fallback failed, got %q", v)
	}
}

// TestAssetLineLocalized checks the ASCII separator and English total count for asset lists.
func TestAssetLineLocalized(t *testing.T) {
	// Below the limit: list every asset with an ASCII comma separator.
	if got := assetLine([]string{"a.example.com", "b.example.com"}, 3); got != "a.example.com, b.example.com" {
		t.Errorf("Separator must be an ASCII comma, got %q", got)
	}
	// Above the limit: show the first limit entries and the total count.
	got := assetLine([]string{"a", "b", "c", "d", "e"}, 2)
	if hasHan(got) {
		t.Errorf("Han characters remain in the asset list: %q", got)
	}
	if !strings.Contains(got, "(5 total)") {
		t.Errorf("Total count must be expressed as '(5 total)', got %q", got)
	}
}
