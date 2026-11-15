package notify

import (
	"strings"
	"testing"
)

// These tests protect English email labels and validation errors from language regressions. Helpers
// live in notify_localized_test.go. Keep input data ASCII so assertions isolate template labels rather
// than user content.

// TestHTMLItemLabelsLocalized exercises all single-email label branches and checks English output.
func TestHTMLItemLabelsLocalized(t *testing.T) {
	m := Message{
		HomeURL: "https://example.com/panel",
		Items: []Item{{
			Name:       "sqli-login", // Title() = Name
			VulnClass:  "injection",  // Must differ from Title() to render the Type line
			Severity:   "high",       // SeverityLabel returns High
			Summary:    "login form is injectable",
			Assets:     []string{"host-a.example.com"},
			DetailURL:  "https://example.com/f/1",
			FromStatus: "pending", // IsStatusChange()=true renders the status change line
			ToStatus:   "fixed",
		}},
	}
	out := htmlBody(m, 0)
	if hasHan(out) {
		t.Errorf("Han characters remain in email body:\n%s", out)
	}
	for _, want := range []string{
		"Status change", "Type", "Assets", "Summary", "View details", "View all on the platform",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Email body is missing label %q:\n%s", want, out)
		}
	}
}

// TestHTMLBatchIntroLocalized checks English digest introductions with and without a time window.
func TestHTMLBatchIntroLocalized(t *testing.T) {
	items := []Item{{Name: "a", Severity: "high"}, {Name: "b", Severity: "low"}}

	withWindow := htmlBatchIntro(Message{Batch: true, WindowMinutes: 30, Items: items})
	assertEnglish(t, "htmlBatchIntro(with window)", withWindow)
	for _, want := range []string{"Last 30 minutes", "new vulnerabilities", "2 new vulnerabilities"} {
		if !strings.Contains(withWindow, want) {
			t.Errorf("Window introduction is missing %q: %q", want, withWindow)
		}
	}

	noWindow := htmlBatchIntro(Message{Batch: true, WindowMinutes: 0, Items: items})
	assertEnglish(t, "htmlBatchIntro(no window)", noWindow)
	if !strings.Contains(noWindow, "2 new vulnerabilities") {
		t.Errorf("Introduction without a window must contain %q: %q", "2 new vulnerabilities", noWindow)
	}
	if strings.Contains(noWindow, "minutes") {
		t.Errorf("Introduction without a window unexpectedly contains 'minutes': %q", noWindow)
	}
}

// TestEmailValidateLocalized checks that four SMTP validation errors use English and identify the
// invalid field. These errors are displayed directly to notification administrators.
func TestEmailValidateLocalized(t *testing.T) {
	cases := []struct {
		name   string
		cfg    map[string]any
		substr string
	}{
		{"missing server address", map[string]any{"port": float64(25), "from": "a@b.c", "to": []any{"d@e.f"}}, "SMTP"},
		{"port out of range", map[string]any{"host": "h"}, "port"},
		{"missing sender", map[string]any{"host": "h", "port": float64(25), "to": []any{"d@e.f"}}, "Sender"},
		{"missing recipient", map[string]any{"host": "h", "port": float64(25), "from": "a@b.c"}, "recipient"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (emailChannel{}).Validate(tc.cfg)
			if err == nil {
				t.Fatalf("Validation must fail: %v", tc.cfg)
			}
			assertEnglish(t, "Validate("+tc.name+")", err.Error())
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("Error must contain %q, got %q", tc.substr, err.Error())
			}
		})
	}
}
