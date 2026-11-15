package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests for English self-update messages in update.go.
// The settings update card renders progress.message, reason, and writeErr
// responses directly. These tests reuse the shared assertEnglishError
// and decodeErrorField helpers.

func TestUpdateMessageConstantsLocalized(t *testing.T) {
	assertEnglishError(t, "updateMsgPreparing", updateMsgPreparing)
	assertEnglishError(t, "updateMsgFailed", updateMsgFailed)
	assertEnglishError(t, "updateMsgStaged", updateMsgStaged)
	assertEnglishError(t, "updateErrInProgress", updateErrInProgress)
	assertEnglishError(t, "updateErrRollbackInProgress", updateErrRollbackInProgress)

	// Fill format placeholders before checking the resulting English messages.
	notRelease := fmt.Sprintf(updateErrNotReleaseFmt, "v0.0.0-dev")
	assertEnglishError(t, "updateErrNotReleaseFmt", notRelease)
	if !strings.Contains(notRelease, "v0.0.0-dev") {
		t.Fatalf("updateErrNotReleaseFmt: version placeholder was not replaced: %q", notRelease)
	}
	latest := fmt.Sprintf(updateErrAlreadyLatestFmt, "v1.2.3")
	assertEnglishError(t, "updateErrAlreadyLatestFmt", latest)
	if !strings.Contains(latest, "v1.2.3") {
		t.Fatalf("updateErrAlreadyLatestFmt: version placeholder was not replaced: %q", latest)
	}
}

// Exercise begin/finish to verify the SSE progress messages. Use a local hub
// to avoid changing the global updHub; no database or network is needed.
func TestUpdateProgressMessagesLocalized(t *testing.T) {
	h := &updateHub{subs: map[chan updateProgress]struct{}{}}

	if !h.begin("v1.2.3") {
		t.Fatal("begin must return true on the first call")
	}
	cur, running := h.snapshot()
	if !running {
		t.Fatal("must be running after begin")
	}
	if cur.Message != updateMsgPreparing {
		t.Fatalf("preparing message mismatch: %q", cur.Message)
	}
	if h.begin("v1.2.4") {
		t.Fatal("begin must return false while running")
	}

	h.finish(errors.New("Download failed"))
	cur, running = h.snapshot()
	if running {
		t.Fatal("running must be cleared after finish")
	}
	if cur.Message != updateMsgFailed {
		t.Fatalf("failure message mismatch: %q", cur.Message)
	}

	h.finish(nil)
	cur, _ = h.snapshot()
	if cur.Message != updateMsgStaged {
		t.Fatalf("ready message mismatch: %q", cur.Message)
	}
}

// Exercise the HTTP 409 rollback-in-progress response without a database.
func TestUpdateRollbackInProgressLocalized(t *testing.T) {
	updHub.mu.Lock()
	prev := updHub.running
	updHub.running = true
	updHub.mu.Unlock()
	defer func() {
		updHub.mu.Lock()
		updHub.running = prev
		updHub.mu.Unlock()
	}()

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/update/rollback", nil)
	rec := httptest.NewRecorder()
	s.updateRollback(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d (body %s)", rec.Code, rec.Body.Bytes())
	}
	msg := decodeErrorField(t, rec.Body.Bytes())
	assertEnglishError(t, "updateRollback in-progress 409", msg)
	if msg != updateErrRollbackInProgress {
		t.Fatalf("rollback refusal message mismatch: %q", msg)
	}
}
