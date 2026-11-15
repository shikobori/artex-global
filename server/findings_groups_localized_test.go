package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests for English user-facing messages in findings_groups.go.
// The shared assertEnglishError assertion checks English source messages.

// TestDeepenFindingBodyTooLargeLocalized checks the HTTP 413 response.
// MaxBytesReader rejects the body before database access, so an empty Server
// is sufficient to exercise this path.
func TestDeepenFindingBodyTooLargeLocalized(t *testing.T) {
	s := &Server{}
	body := `{"description":"` + strings.Repeat("a", 33<<10) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/exploration/findings/1/deepen", strings.NewReader(body))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	s.deepenFinding(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v (body %q)", err, rec.Body.String())
	}
	const want = "The request body is too large"
	if resp.Error != want {
		t.Fatalf("response message = %q, want %q", resp.Error, want)
	}
	assertEnglishError(t, "body_too_large", resp.Error)
}

// TestFindingFollowUpAuditSummaryLocalized checks the follow-up intent summary.
// AddFindingFollowUpIntent requires a database transaction, so this test checks
// the constant directly. It is an activity timeline label, separate from the
// user-provided description in the agent intent payload.
func TestFindingFollowUpAuditSummaryLocalized(t *testing.T) {
	assertEnglishError(t, "follow_up_summary", auditFindingFollowUpSummary)
}
