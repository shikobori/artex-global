package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression coverage for English conversation API errors. assertEnglishError checks
// that source messages are English.
//
// TestConversationErrorConstantsLocalized checks all twelve response constants.
// The handlers require PostgreSQL, so constants are tested directly. Format
// strings containing %d are expanded before checking the final message.
func TestConversationErrorConstantsLocalized(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"request_too_large", convErrRequestTooLarge},
		{"agent_key_empty", convErrAgentKeyEmpty},
		{"agent_key_too_long", fmt.Sprintf(convErrAgentKeyTooLong, maxConversationAgentKeyRunes)},
		{"agent_not_found", convErrAgentNotFound},
		{"llm_profile", convErrLLMProfile},
		{"title_too_long", fmt.Sprintf(convErrTitleTooLong, maxConversationTitleRunes)},
		{"title_or_pinned", convErrTitleOrPinned},
		{"title_empty", convErrTitleEmpty},
		{"bad_conv_id", convErrBadConvID},
		{"ids_count", fmt.Sprintf(convErrIDsCount, maxConversationDeleteBatch)},
		{"message_empty", convErrMessageEmpty},
		{"busy", convErrBusy},
	}
	for _, c := range cases {
		assertEnglishError(t, c.name, c.msg)
	}
}

// TestDecodeConversationRequestTooLargeLocalized checks the actual HTTP response.
// decodeConversationRequest needs no Server or DB, so this verifies that an
// oversized request returns HTTP 413 with the English error constant.
func TestDecodeConversationRequestTooLargeLocalized(t *testing.T) {
	// Valid JSON exceeding 64 KB causes MaxBytesReader to fail while reading.
	body := `{"title":"` + strings.Repeat("a", maxConversationRequestBytes+1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/conversations", strings.NewReader(body))
	rec := httptest.NewRecorder()
	var dst struct {
		Title string `json:"title"`
	}
	if decodeConversationRequest(rec, req, &dst) {
		t.Fatal("decodeConversationRequest returned true for an oversized body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status code = %d, want = %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v (body=%q)", err, rec.Body.String())
	}
	if resp.Error != convErrRequestTooLarge {
		t.Fatalf("response error = %q, want = %q", resp.Error, convErrRequestTooLarge)
	}
	assertEnglishError(t, "decode.too_large.response", resp.Error)
}

// TestConversationDefaultTitlesLocalized checks that both default titles are
// English and distinct. convDefaultTitle is also the automatic-title sentinel,
// so a regression would affect conversation lists and deletion dialogs.
func TestConversationDefaultTitlesLocalized(t *testing.T) {
	assertEnglishError(t, "default_title", convDefaultTitle)
	assertEnglishError(t, "attachment_title", convAttachmentTitle)
	if convDefaultTitle == convAttachmentTitle {
		t.Fatal("default conversation and attachment titles must differ")
	}
}

// TestIsDefaultConversationTitle pins automatic-title detection. Empty and default
// titles qualify; user-supplied titles do not. Sharing the creation constant with
// the sentinel prevents mismatches from disabling automatic titles.
func TestIsDefaultConversationTitle(t *testing.T) {
	if !isDefaultConversationTitle("") {
		t.Fatal("an empty title must qualify for automatic naming")
	}
	if !isDefaultConversationTitle(convDefaultTitle) {
		t.Fatalf("the creation default %q must qualify for automatic naming", convDefaultTitle)
	}
	if isDefaultConversationTitle("사용자가 지은 제목") {
		t.Fatal("a user-supplied title must not qualify for automatic naming")
	}
}

// TestConversationRetestReasonsLocalized checks the three English termination
// reasons stored in finding_retests.error and displayed as item.error in the
// revalidation panel.
func TestConversationRetestReasonsLocalized(t *testing.T) {
	assertEnglishError(t, "retest_failed_to_start", convRetestFailedToStart)
	assertEnglishError(t, "retest_status_read_failed", convRetestStatusReadFailed)
	assertEnglishError(t, "retest_stopped_or_closed", convRetestStoppedOrClosed)
}

// TestTranscriptErrorSummaryLocalized pins the transcript error wrapper. Chat
// turns and main-agent errors use the same English format and preserve the inner
// error verbatim. Wrapper punctuation must use ASCII parentheses and colons.
func TestTranscriptErrorSummaryLocalized(t *testing.T) {
	cases := []struct {
		name  string
		label string
		errm  string
		want  string
	}{
		{"chat_turn", "", "connection reset", "(error: connection reset)"},
		{"main_agent", "Main agent", "connection reset", "(Main agent error: connection reset)"},
	}
	for _, c := range cases {
		got := transcriptErrorSummary(c.label, c.errm)
		if got != c.want {
			t.Fatalf("%s: transcriptErrorSummary = %q, want = %q", c.name, got, c.want)
		}
		if !strings.Contains(got, c.errm) {
			t.Fatalf("%s: the original error text was not preserved: %q", c.name, got)
		}
		// Check that wrapper parentheses and colons use ASCII punctuation.
		if strings.ContainsAny(got, "（）：") {
			t.Fatalf("%s: full-width punctuation remains: %q", c.name, got)
		}
		// Check the English wrapper; the inner error is fixed ASCII text here.
		wrapper := strings.ReplaceAll(got, c.errm, "")
		assertEnglishError(t, c.name+".wrapper", wrapper)
	}
}
