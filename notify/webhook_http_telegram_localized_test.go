package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// These tests protect English default Telegram labels and batch titles, Webhook validation errors, HTTP
// transport errors, and shared URL validation. Helpers are in notify_localized_test.go. ASCII fixture data
// isolates renderer labels from user content.

// TestTelegramItemLabelsLocalized exercises every single-item label branch and verifies English labels
// without CJK characters.
func TestTelegramItemLabelsLocalized(t *testing.T) {
	m := Message{
		HomeURL: "https://example.com/panel",
		Items: []Item{{
			Name:       "sqli-login", // Title() = Name
			VulnClass:  "injection",  // Must differ from Title() to render the type line
			Severity:   "high",       // SeverityLabel → High
			Summary:    "login form is injectable",
			Assets:     []string{"host-a.example.com"},
			DetailURL:  "https://example.com/f/1",
			FromStatus: "pending", // IsStatusChange()=true renders the status-change line
			ToStatus:   "fixed",
		}},
	}
	out, kept := telegramHTML(m)
	if kept != 1 {
		t.Fatalf("Single-item message should report one item, got %d", kept)
	}
	if hasHan(out) {
		t.Errorf("Telegram body contains CJK characters:\n%s", out)
	}
	for _, want := range []string{"Status change", "Type", "Assets", "Summary", "View details"} {
		if !strings.Contains(out, want) {
			t.Errorf("Telegram body is missing label %q:\n%s", want, out)
		}
	}
}

// TestTelegramBatchTitleLocalized checks English overflow and time-window title branches, consistent with
// markdownTitle across channels.
func TestTelegramBatchTitleLocalized(t *testing.T) {
	items := []Item{{Name: "a", Severity: "high"}, {Name: "b", Severity: "low"}}

	// Overflow with a time window: summary, total, included count, remainder, next message, and recent
	// minutes.
	over := telegramBatchTitle(Message{WindowMinutes: 30}, items, 5)
	assertEnglish(t, "telegramBatchTitle(overflow)", over)
	for _, want := range []string{"Vulnerability summary · 5 total", "showing first 2", "remaining 3", "next message", "Last 30 minutes"} {
		if !strings.Contains(over, want) {
			t.Errorf("Overflow title is missing %q: %q", want, over)
		}
	}

	// Without overflow or a time window, there must be no next-message notice.
	full := telegramBatchTitle(Message{}, items, 2)
	assertEnglish(t, "telegramBatchTitle(full)", full)
	if !strings.Contains(full, "Vulnerability summary · 2 total") {
		t.Errorf("Full title should contain 'Vulnerability summary · 2 total': %q", full)
	}
	if strings.Contains(full, "next message") {
		t.Errorf("Title contains a next-message notice without overflow: %q", full)
	}
}

// TestTelegramValidateLocalized checks English missing-field errors that identify Bot Token and Chat ID.
func TestTelegramValidateLocalized(t *testing.T) {
	if err := (telegramChannel{}).Validate(map[string]any{}); err == nil {
		t.Fatal("Missing bot_token should fail validation")
	} else {
		assertEnglish(t, "telegram Validate(bot_token)", err.Error())
		if !strings.Contains(err.Error(), "Bot Token") {
			t.Errorf("Should identify missing Bot Token: %q", err.Error())
		}
	}
	if err := (telegramChannel{}).Validate(map[string]any{"bot_token": "t"}); err == nil {
		t.Fatal("Missing chat_id should fail validation")
	} else {
		assertEnglish(t, "telegram Validate(chat_id)", err.Error())
		if !strings.Contains(err.Error(), "Chat ID") {
			t.Errorf("Should identify missing Chat ID: %q", err.Error())
		}
	}
}

// TestWebhookValidateLocalized checks three English validation errors that identify their causes.
func TestWebhookValidateLocalized(t *testing.T) {
	if err := (webhookChannel{}).Validate(map[string]any{}); err == nil {
		t.Fatal("Missing url should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(url)", err.Error())
		if !strings.Contains(err.Error(), "Destination URL") {
			t.Errorf("Should identify missing destination URL: %q", err.Error())
		}
	}
	// Unsupported method: English error with the allowed methods.
	if err := (webhookChannel{}).Validate(map[string]any{"url": "https://example.com/hook", "method": "DELETE"}); err == nil {
		t.Fatal("DELETE should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(method)", err.Error())
		if !strings.Contains(err.Error(), "GET/POST/PUT/PATCH") {
			t.Errorf("Should list allowed methods: %q", err.Error())
		}
	}
	// Template syntax error: English wrapper around the Go template error.
	if err := (webhookChannel{}).Validate(map[string]any{"url": "https://example.com/hook", "body_template": "{{"}); err == nil {
		t.Fatal("Broken template should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(template)", err.Error())
		if !strings.Contains(err.Error(), "template") {
			t.Errorf("Should identify the template error: %q", err.Error())
		}
	}
}

// TestValidateHTTPURLLocalized checks English errors in the URL helper shared by Telegram, Webhook,
// DingTalk, Feishu, and WeCom, avoiding mixed-language error wrappers.
func TestValidateHTTPURLLocalized(t *testing.T) {
	// Unsupported scheme.
	if err := validateHTTPURL("ftp://example.com/x"); err == nil {
		t.Fatal("ftp scheme should be rejected")
	} else {
		assertEnglish(t, "validateHTTPURL(scheme)", err.Error())
		if !strings.Contains(err.Error(), "http") {
			t.Errorf("Should list supported schemes (http/https): %q", err.Error())
		}
	}
	// Missing hostname.
	if err := validateHTTPURL("http://"); err == nil {
		t.Fatal("Address without a host should be rejected")
	} else {
		assertEnglish(t, "validateHTTPURL(host)", err.Error())
	}
}

// TestHTTPLocalTargetBlockedLocalized checks English local/link-local rejection errors and the opt-in
// environment variable.
func TestHTTPLocalTargetBlockedLocalized(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "") // Explicitly disable opt-in to exercise rejection
	err := blockInternalDial("tcp", "127.0.0.1:25", nil)
	if err == nil {
		t.Fatal("Local addresses should be blocked by default")
	}
	assertEnglish(t, "blockInternalDial", err.Error())
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("Rejection should explain opt-in (%s): %q", AllowLocalTargetsEnv, err.Error())
	}
}

// TestHTTPRedactHelpersLocalized checks English redacted-address placeholders and the unknown-error
// transport branch.
func TestHTTPRedactHelpersLocalized(t *testing.T) {
	if got := redactRequestTarget(""); !strings.Contains(got, "unable to parse") {
		t.Errorf("Unparseable address should use an English placeholder, got %q", got)
	}
	// Exercise *url.Error with nil Err.
	got := redactTransportError(&url.Error{Op: "Get", URL: "http://api.example", Err: nil})
	if hasHan(got) {
		t.Errorf("Redacted transport error contains CJK characters: %q", got)
	}
	if !strings.Contains(got, "Unknown error") {
		t.Errorf("nil Err should produce 'Unknown error', got %q", got)
	}
}

// TestHTTPStatusErrorsLocalized checks English rejection, server-error, and throttling messages through
// real doJSON requests. Explicitly allow the localhost httptest receiver here.
func TestHTTPStatusErrorsLocalized(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "1")
	cases := []struct {
		code int
		want string
	}{
		{http.StatusForbidden, "rejected request"},
		{http.StatusInternalServerError, "server error"},
		{http.StatusTooManyRequests, "rate limited"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte("body"))
		}))
		_, err := doJSON(context.Background(), http.MethodPost, srv.URL, nil, map[string]any{"a": 1})
		srv.Close()
		if err == nil {
			t.Fatalf("HTTP %d should return an error", tc.code)
		}
		assertEnglish(t, "doJSON(HTTP status)", err.Error())
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("HTTP %d error should contain %q, got %q", tc.code, tc.want, err.Error())
		}
	}
}
