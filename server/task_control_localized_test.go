package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Regression coverage for English task and intent control API errors. Reuses the
// shared assertEnglishError helper and decodeErrorField from category tests.
//
// TestTaskControlErrorConstantsLocalized checks ten English response constants.
// applyIntentControl requires Store.GetNode, so its six messages are checked as
// constants without a database. The tests below exercise task control and batch
// handlers through actual function calls and HTTP response bodies.
func TestTaskControlErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"task_deleting":          errTaskCtrlDeleting,
		"task_terminal_pause":    errTaskCtrlTerminalPause,
		"task_already_paused":    errTaskCtrlAlreadyPaused,
		"batch_size":             errTaskCtrlBatchSizeFmt,
		"intent_inherited":       errIntentCtrlInheritedReadonly,
		"intent_only_running":    errIntentCtrlOnlyRunningPause,
		"intent_only_paused":     errIntentCtrlOnlyPausedResume,
		"intent_state_conflict":  errIntentCtrlStateConflictFmt,
		"intent_only_deletable":  errIntentCtrlOnlyDeletable,
		"intent_reason_required": errIntentCtrlReasonRequired,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// TestTaskControlApplyResponsesLocalized exercises pause errors before any DB or
// engine operation. Each path reads the task map, deletion barrier, and in-memory
// lifecycle fields, so one task and an empty Engine suffice. The successful pause
// path requires ApplyTaskPause and is outside this test.
func TestTaskControlApplyResponsesLocalized(t *testing.T) {
	// Terminal task: isTerminalStatus(done) branch.
	terminalServer := func() (*Server, *Task) {
		tk := &Task{ID: "1", Status: "done"}
		return &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}, tk
	}
	// Already paused running task: lifecycle.Paused branch.
	pausedServer := func() (*Server, *Task) {
		tk := &Task{ID: "1", Status: "running", Paused: true}
		return &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}, tk
	}
	// Task with an active deletion barrier: IsDeleting branch.
	deletingServer := func() (*Server, *Task) {
		tk := &Task{ID: "1", Status: "running"}
		s := &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}
		s.engine.deleting.Store("1", true)
		return s, tk
	}

	cases := []struct {
		name  string
		setup func() (*Server, *Task)
		want  string
	}{
		{"terminal", terminalServer, errTaskCtrlTerminalPause},
		{"already-paused", pausedServer, errTaskCtrlAlreadyPaused},
		{"deleting", deletingServer, errTaskCtrlDeleting},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, tk := c.setup()
			_, err := s.applyTaskControl(tk, "pause")
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if err.Error() != c.want {
				t.Fatalf("returned error = %q, want = %q", err.Error(), c.want)
			}
			assertEnglishError(t, c.name, err.Error())
		})
	}
}

// TestControlTasksBatchResponsesLocalized checks actual HTTP response bodies.
// Empty task_ids returns HTTP 400 before manager access, so &Server{} suffices.
// Pausing one terminal task returns HTTP 200 with items[].error, verifying that
// applyTaskControl errors propagate to the batch JSON response.
func TestControlTasksBatchResponsesLocalized(t *testing.T) {
	t.Run("size-error", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/control",
			strings.NewReader(`{"task_ids":[],"action":"pause"}`))
		rec := httptest.NewRecorder()
		s.controlTasksBatch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status code = %d, want = 400 (body %q)", rec.Code, rec.Body.String())
		}
		got := decodeErrorField(t, rec.Body.Bytes())
		want := fmt.Sprintf(errTaskCtrlBatchSizeFmt, maxBatchControlIDs)
		if got != want {
			t.Fatalf("response message = %q, want = %q", got, want)
		}
		if !strings.Contains(got, "100") {
			t.Fatalf("the message does not include the size limit of 100: %q", got)
		}
		assertEnglishError(t, "size-error", got)
	})

	t.Run("item-error-surfaces", func(t *testing.T) {
		tk := &Task{ID: "1", Status: "done"}
		s := &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/control",
			strings.NewReader(`{"task_ids":["1"],"action":"pause"}`))
		rec := httptest.NewRecorder()
		s.controlTasksBatch(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want = 200 (body %q)", rec.Code, rec.Body.String())
		}
		var out struct {
			Items []struct {
				ID    string `json:"id"`
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("failed to parse response JSON: %v (body %q)", err, rec.Body.String())
		}
		if len(out.Items) != 1 {
			t.Fatalf("item count = %d, want = 1", len(out.Items))
		}
		if out.Items[0].OK || out.Items[0].Error != errTaskCtrlTerminalPause {
			t.Fatalf("items[0] = %+v, error want = %q", out.Items[0], errTaskCtrlTerminalPause)
		}
		assertEnglishError(t, "item-error", out.Items[0].Error)
	})
}

// TestIntentStateConflictWrapsSentinel checks the English conflict message while
// preserving %w wrapping of db.ErrIntentStateConflict for errors.Is. The paused
// enum value must remain unchanged.
func TestIntentStateConflictWrapsSentinel(t *testing.T) {
	err := fmt.Errorf(errIntentCtrlStateConflictFmt, db.ErrIntentStateConflict)
	if !errors.Is(err, db.ErrIntentStateConflict) {
		t.Fatalf("errors.Is(.., ErrIntentStateConflict) = false, %%w wrapping is broken: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "paused") {
		t.Fatalf("the paused enum value was not preserved in the message: %q", err.Error())
	}
	assertEnglishError(t, "state-conflict", err.Error())
}
