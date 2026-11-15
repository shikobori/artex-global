//go:build !embedui

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWebUIStubErrorLocalized guards the no-embed stub's HTTP response: a request
// served by the default (!embedui) build must get an English 404 body.
// This response uses http.Error rather than writeErr, so it needs its own check
// to detect regressions to non-English source messages.
func TestWebUIStubErrorLocalized(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	(&Server{}).webuiHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status code is not 404: %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	assertEnglishError(t, "webui.stub", body)
	// Command references must remain unchanged.
	for _, want := range []string{"next dev", "-tags embedui"} {
		if !strings.Contains(body, want) {
			t.Fatalf("command reference %q is missing from response: %q", want, body)
		}
	}
}
