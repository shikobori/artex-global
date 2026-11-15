package server

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Regression tests for English notification-settings API responses and test messages.
// The shared assertEnglishError helper checks English source messages.

// TestNotifyErrorConstantsLocalized checks the ten response constants and three test-message
// constants for English source text, detecting regressions to other source languages.
func TestNotifyErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"notifyErrBadJSON":         notifyErrBadJSON,
		"notifyErrKindInvalidFmt":  fmt.Sprintf(notifyErrKindInvalidFmt, "email / webhook"),
		"notifyErrNameMissing":     notifyErrNameMissing,
		"notifyErrNameEmpty":       notifyErrNameEmpty,
		"notifyErrModeInvalid":     notifyErrModeInvalid,
		"notifyErrRateNegative":    notifyErrRateNegative,
		"notifyErrChannelID":       notifyErrChannelID,
		"notifyErrKindUnregFmt":    fmt.Sprintf(notifyErrKindUnregFmt, "bogus"),
		"notifyErrDeliveryID":      notifyErrDeliveryID,
		"notifyErrChannelNotFound": notifyErrChannelNotFound,
		"notifyTestName":           notifyTestName,
		"notifyTestClass":          notifyTestClass,
		"notifyTestSummary":        notifyTestSummary,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// TestNotifyChannelLookupErrLocalized exercises the missing-channel → 404 response path.
// notifyChannelLookupErr needs only w and err, without a Server or database. Passing the sentinel
// error verifies that the 404 response contains the expected English constant.
func TestNotifyChannelLookupErrLocalized(t *testing.T) {
	rec := httptest.NewRecorder()
	notifyChannelLookupErr(rec, db.ErrNotificationChannelNotFound)
	if rec.Code != 404 {
		t.Fatalf("status code = %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
	got := decodeErrorField(t, rec.Body.Bytes())
	if got != notifyErrChannelNotFound {
		t.Fatalf("response message = %q, want %q", got, notifyErrChannelNotFound)
	}
	assertEnglishError(t, "channel_not_found", got)
}

// TestNotifyTestMessageLocalized builds the connection-test message with a pure function and
// checks its user-facing title, category, and summary in English, without a DB or network.
func TestNotifyTestMessageLocalized(t *testing.T) {
	const base = "https://artex.example.test"
	msg := notifyTestMessage(base)
	if len(msg.Items) != 1 {
		t.Fatalf("test message item count = %d, want 1", len(msg.Items))
	}
	item := msg.Items[0]
	assertEnglishError(t, "test.name", item.Name)
	assertEnglishError(t, "test.class", item.VulnClass)
	assertEnglishError(t, "test.summary", item.Summary)
	if msg.HomeURL != base || item.DetailURL != base {
		t.Fatalf("links not preserved: HomeURL=%q DetailURL=%q (want %q)", msg.HomeURL, item.DetailURL, base)
	}
}
