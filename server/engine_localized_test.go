package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Regression tests for English task-control and intent-intervention errors.
// Reuse assertEnglishError to check English responses.

//
// Call graph (see the constants in engine.go):
// - The four ControlWork errors (missing running work, control in progress, cancelled wait,
//   and timed-out wait) reach applyIntentControl and controlIntent, then writeErr 409.
//   These are user-facing paths rather than actool calls.
// - The two runDetachedIntent errors (Worker not ready and resume CAS conflict) reach
//   sendWorkerMessage and writeErr.
// - SteerWork, KillWork, and transitionIntentState messages are covered elsewhere.

// TestControlWorkNoRunningWorkErrorLocalized runs ControlWork without a database or engine.
// The run==nil branch only reads e.work, so NewEngine(nil) reaches it. Also verify that
// wrapping errWorkControlConflict with %w preserves errors.Is.
func TestControlWorkNoRunningWorkErrorLocalized(t *testing.T) {
	e := NewEngine(nil)
	err := e.ControlWork(context.Background(), 42, "pause")
	if err == nil {
		t.Fatal("ControlWork returned nil with no running work")
	}
	if !errors.Is(err, errWorkControlConflict) {
		t.Fatalf("errors.Is(err, errWorkControlConflict) = false, err=%v", err)
	}
	assertEnglishError(t, "control_work_no_running", err.Error())
}

// TestEngineUserFacingErrorsLocalized pins the remaining user-facing format strings behind
// engine/database gates or goroutine timing. Fill them with representative arguments before
// checking their language.
func TestEngineUserFacingErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"work_control_busy", fmt.Errorf(errWorkControlBusyFmt, errWorkControlConflict, int64(42), "pause").Error()},
		{"work_control_wait", fmt.Errorf(errWorkControlWaitFmt, int64(42), "pause", context.DeadlineExceeded).Error()},
		{"detached_worker_not_ready", errDetachedWorkerNotReady},
		{"detached_state_conflict", fmt.Errorf(errIntentCtrlStateConflictFmt, db.ErrIntentStateConflict).Error()},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
