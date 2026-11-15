package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// asciiStatusChangeItem supplies ASCII data so language assertions detect label regressions rather
// than user content.
func asciiStatusChangeItem() Item {
	return Item{
		FindingID:  7,
		Name:       "login-flaw",
		VulnClass:  "SQLi",
		Severity:   "high",
		Summary:    "SQL injection via q param",
		Assets:     []string{"a.example.com"},
		DetailURL:  "https://platform.example/finding/7",
		FromStatus: "pending",
		ToStatus:   "fixed",
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Failed to serialize card: %v", err)
	}
	return string(b)
}

// TestFeishuItemLinesLocalized verifies English field labels match markdown.go. Feishu maintains its
// own labels, so these assertions catch regressions independent of the shared renderer.
func TestFeishuItemLinesLocalized(t *testing.T) {
	got := feishuItemLines(asciiStatusChangeItem())
	assertEnglish(t, "feishuItemLines", got)
	for _, want := range []string{
		"**Status change**: Pending → Fixed",
		"**Type**: SQLi",
		"**Assets**: a.example.com",
		"**Summary**: SQL injection via q param",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Feishu single-item body must contain %q:\n%s", want, got)
		}
	}
}

// TestFeishuCardButtonsLocalized verifies English button labels and no CJK text with ASCII input.
func TestFeishuCardButtonsLocalized(t *testing.T) {
	// Single item: show the View details button when DetailURL is present.
	single, _ := feishuCard(Message{Items: []Item{asciiStatusChangeItem()}})
	singleJSON := mustJSON(t, single)
	if hasHan(singleJSON) {
		t.Errorf("Han characters remain in single-item Feishu card:\n%s", singleJSON)
	}
	if !strings.Contains(singleJSON, "View details") {
		t.Errorf("Single-item Feishu card must include a View details button:\n%s", singleJSON)
	}

	// Batch: show View all on the platform when HomeURL is present.
	batch, _ := feishuCard(Message{Batch: true, HomeURL: "https://platform.example", Items: hanFreeItems(2)})
	batchJSON := mustJSON(t, batch)
	if hasHan(batchJSON) {
		t.Errorf("Han characters remain in batch Feishu card:\n%s", batchJSON)
	}
	if !strings.Contains(batchJSON, "View all on the platform") {
		t.Errorf("Batch Feishu card must include a View all on the platform button:\n%s", batchJSON)
	}
}

// TestDingTalkActionCardButtonLocalized checks the English ActionCard button through Send with a fake
// receiver.
func TestDingTalkActionCardButtonLocalized(t *testing.T) {
	var singleTitle string
	srv := capturePost(t, `{"errcode":0,"errmsg":"ok"}`, func(_ *testing.T, body map[string]any, _ *http.Request) {
		card, _ := body["actionCard"].(map[string]any)
		singleTitle, _ = card["singleTitle"].(string)
	})
	m := Message{Items: []Item{asciiStatusChangeItem()}}
	if _, err := (dingTalkChannel{}).Send(context.Background(), map[string]any{"webhook": srv.URL}, m); err != nil {
		t.Fatalf("Delivery failed: %v", err)
	}
	if singleTitle != "View details" {
		t.Errorf("DingTalk ActionCard singleTitle must be 'View details', got %q", singleTitle)
	}
}

// TestChinaPlatformValidateLocalized checks English validation errors and preserves the Webhook
// spelling exposed by configuration and test APIs.
func TestChinaPlatformValidateLocalized(t *testing.T) {
	for _, kind := range []string{KindDingTalk, KindFeishu, KindWeCom} {
		ch, ok := Get(kind)
		if !ok {
			t.Fatalf("Channel %s is not registered", kind)
		}

		// Empty configuration: missing Webhook URL.
		missing := ch.Validate(map[string]any{})
		if missing == nil {
			t.Fatalf("%s: empty configuration must fail validation", kind)
		}
		assertEnglish(t, kind+" missing", missing.Error())
		if !strings.Contains(missing.Error(), "Webhook") || !strings.Contains(missing.Error(), "missing") {
			t.Errorf("%s: expected 'Webhook URL is missing', got %q", kind, missing)
		}

		// Invalid address (ftp): invalid Webhook URL.
		bad := ch.Validate(map[string]any{"webhook": "ftp://x"})
		if bad == nil {
			t.Fatalf("%s: ftp URL must fail validation", kind)
		}
		if hasHan(bad.Error()) {
			t.Errorf("%s: Han characters remain in invalid URL error: %q", kind, bad)
		}
		if !strings.Contains(bad.Error(), "Invalid Webhook URL") {
			t.Errorf("%s: invalid URL error must start with 'Invalid Webhook URL', got %q", kind, bad)
		}
	}
}

// TestChinaPlatformSendErrorsLocalized verifies English delivery errors stored in last_error. Platform
// names match UI labels. Server error text is ASCII so language assertions isolate the message
// template.
func TestChinaPlatformSendErrorsLocalized(t *testing.T) {
	// Application errors: HTTP 200 with a nonzero errcode/code in the body.
	bizCases := []struct {
		kind     string
		resp     string
		wantSubs []string
	}{
		{KindDingTalk, `{"errcode":310000,"errmsg":"keyword not matched"}`, []string{"DingTalk error", "310000"}},
		{KindFeishu, `{"code":19021,"msg":"sign error"}`, []string{"Feishu error", "19021"}},
		{KindWeCom, `{"errcode":45009,"errmsg":"freq out of limit"}`, []string{"WeCom rate limited", "45009"}},
		{KindWeCom, `{"errcode":93000,"errmsg":"invalid webhook"}`, []string{"WeCom error", "93000"}},
	}
	for _, tc := range bizCases {
		srv := capturePost(t, tc.resp, nil)
		ch, ok := Get(tc.kind)
		if !ok {
			t.Fatalf("Channel %s is not registered", tc.kind)
		}
		_, err := ch.Send(context.Background(), map[string]any{"webhook": srv.URL}, singleMsg())
		if err == nil {
			t.Fatalf("%s: response %s must be an error", tc.kind, tc.resp)
		}
		if hasHan(err.Error()) {
			t.Errorf("%s: Han characters remain in delivery error: %q", tc.kind, err)
		}
		for _, sub := range tc.wantSubs {
			if !strings.Contains(err.Error(), sub) {
				t.Errorf("%s: delivery error must contain %q, got %q", tc.kind, sub, err)
			}
		}
	}

	// Parsing failure when the response is not JSON.
	parseCases := []struct {
		kind     string
		platform string
	}{
		{KindDingTalk, "DingTalk"},
		{KindFeishu, "Feishu"},
		{KindWeCom, "WeCom"},
	}
	for _, pc := range parseCases {
		srv := capturePost(t, `not-json`, nil)
		ch, ok := Get(pc.kind)
		if !ok {
			t.Fatalf("Channel %s is not registered", pc.kind)
		}
		_, err := ch.Send(context.Background(), map[string]any{"webhook": srv.URL}, singleMsg())
		if err == nil {
			t.Fatalf("%s: non-JSON response must be an error", pc.kind)
		}
		want := "Failed to parse " + pc.platform + " response"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: parse error must contain %q, got %q", pc.kind, want, err)
		}
	}
}
