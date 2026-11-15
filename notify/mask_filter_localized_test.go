package notify

import (
	"errors"
	"strings"
	"testing"
)

// Pin English user-facing validation errors from mask.go/filter.go, returned by the notification API
// as HTTP 400. Shared language helpers live in notify_localized_test.go.

// TestFilterValidateLocalized checks the invalid min_severity error exposed by create/update APIs.
func TestFilterValidateLocalized(t *testing.T) {
	err := Filter{MinSeverity: "hgih"}.Validate()
	if err == nil {
		t.Fatal("Expected an error for invalid minimum severity")
	}
	assertEnglish(t, "Filter.Validate", err.Error())
	// Keep low/medium/high/critical unchanged because they are configuration enum tokens.
	for _, tok := range []string{"low", "medium", "high", "critical"} {
		if !strings.Contains(err.Error(), tok) {
			t.Errorf("Severity token %q is missing: %q", tok, err.Error())
		}
	}
	// Valid and empty values must pass.
	if err := (Filter{MinSeverity: "high"}).Validate(); err != nil {
		t.Errorf("high is valid but returned an error: %v", err)
	}
	if err := (Filter{}).Validate(); err != nil {
		t.Errorf("Empty minimum severity is valid but returned an error: %v", err)
	}
}

// TestPrepareConfigUpdateUnknownKindLocalized checks the HTTP 400 error for updating an unregistered
// channel type.
func TestPrepareConfigUpdateUnknownKindLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("definitely-not-a-channel", map[string]any{}, map[string]any{})
	if err == nil {
		t.Fatal("Expected an error for unregistered channel type")
	}
	assertEnglish(t, "PrepareConfigUpdate unknown kind", err.Error())
}

// TestDestinationChangedErrorLocalized triggers the real update path by changing only webhook.url and
// omitting headers credentials; the server returns HTTP 400.
func TestDestinationChangedErrorLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("webhook",
		map[string]any{
			"url":     "https://old.example.com/hook",
			"headers": map[string]any{"Authorization": "Bearer real-token"},
		},
		map[string]any{"url": "https://attacker.example.com/hook"},
	)
	if err == nil {
		t.Fatal("Expected an error for changing destination without explicit credentials")
	}
	var de *ErrDestinationChangedWithoutCredentials
	if !errors.As(err, &de) {
		t.Fatalf("Unexpected error type: %T", err)
	}
	assertEnglish(t, "ErrDestinationChangedWithoutCredentials", err.Error())
	// Changed destination keys and missing credential keys must appear verbatim because they identify
	// configuration fields.
	if !strings.Contains(err.Error(), "url") || !strings.Contains(err.Error(), "headers") {
		t.Errorf("Field key names are missing: %q", err.Error())
	}
}

// TestRejectMaskedInContainersLocalized triggers rejection by embedding a masked sentinel inside the
// credential headers object.
func TestRejectMaskedInContainersLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("webhook",
		map[string]any{},
		map[string]any{"headers": map[string]any{"Authorization": MaskedPrefix + ":…abc123"}},
	)
	if err == nil {
		t.Fatal("Expected an error for a masked sentinel embedded in a structure")
	}
	assertEnglish(t, "rejectMaskedInContainers", err.Error())
	// Show the literal sentinel (__masked__) so operators can identify the invalid value.
	if !strings.Contains(err.Error(), MaskedPrefix) {
		t.Errorf("Error is missing the masked sentinel: %q", err.Error())
	}
}
