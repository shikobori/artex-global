package agent

import (
	"context"
	"errors"
	"fmt"
)

// AbortCause names why an agent run's context was cancelled. Every cancellation
// site should attach one so the activity trace can report the real initiator.
type AbortCause struct {
	Code  string
	Short string
	Text  string
}

func (c *AbortCause) Error() string { return c.Text }

func cause(code, short, text string) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: text}
}

// Causef builds a cause that includes runtime-specific detail.
func Causef(code, short, format string, args ...any) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: fmt.Sprintf(format, args...)}
}

var (
	// Task-level execution context.
	AbortPausedByUser = cause("paused_by_user", "User paused the task",
		"The user paused the task through the task control API (POST /api/tasks/{id}/control, action=pause). This Planner/Worker run was explicitly cancelled; running intents return to frontier(open) and are reclaimed and restarted from the beginning when the task resumes")
	AbortPausedByOrchestrator = cause("paused_by_orchestrator", "Orchestrator paused the task",
		"The orchestrator Agent called pause_task to pause this task. This Planner/Worker run was explicitly cancelled; running intents return to frontier(open) and run again after resumption")
	AbortTaskDeleted = cause("task_deleted", "Task deleted",
		"The task is being deleted (DELETE /api/tasks/{id}); the deletion barrier has cancelled its running Planner, Workers, and main Agent. The results of this run will no longer be used")
	AbortPausedOnReload = cause("paused_on_reload", "Backend restored the paused task state",
		"On startup, the backend restored the paused task state persisted in the database. This run was cancelled; normally no Agent is running during restoration")
	AbortGoalMet = cause("goal_met", "Planner determined the goal was met",
		"The Planner determined the task goal was met and set the task to done, then cancelled Workers still running; these intents are marked stopped rather than failed")
	AbortSettleDrainTimeout = cause("settle_drain_timeout", "Task timeout drain period expired",
		"After the task reached timeout, it waited for running Workers to wrap up gracefully, but the 90-second drain grace period was insufficient, so a hard cancellation was issued; intents are marked exhausted, and facts and assets already written during wrap-up are retained")

	// Per-work context.
	AbortKilledByPlanner = cause("killed_by_planner", "Planner stopped this intent",
		"The Planner called kill_work to explicitly stop this intent, usually because it was off track or no longer worth pursuing; the intent is marked stopped and will not be reclaimed automatically")
	AbortWorkPausedByUser = cause("work_paused_by_user", "User paused this Worker intent",
		"The user paused the running Worker. This call was cancelled and the intent changed to paused; all registered intents, facts, findings, and activity records are retained, and execution restarts from the beginning after resumption")
	AbortWorkCancelledByUser = cause("work_cancelled_by_user", "User deleted this Worker intent",
		"The user deleted the running Worker. This call was cancelled; after the Worker leaves the write section, the server handles the intent according to the selected deletion mode: soft deletion only marks it deleted and retains all outputs, while hard deletion cascades to the intent and downstream nodes supported solely by it")
	AbortWorkFinished = cause("work_finished", "Worker finished and released context",
		"The Worker finished normally and the engine released its context resources in detachWork. This is not an interruption; if it appears in an interruption message, cancellation raced with the completion event")
	AbortPausedRaceGuard = cause("paused_race_guard", "New run rejected while task is paused",
		"While the task is paused, the engine refuses to issue new execution contexts to prevent a race between claim and pause from starting Workers; claimed intents return to frontier")

	// Main Agent and standalone conversation contexts.
	AbortChatStoppedByUser = cause("chat_stopped_by_user", "User stopped this conversation turn",
		"The user clicked Stop to explicitly abort this turn of the main Agent or conversation Agent. Activity records already produced are retained, and the next message can be sent")
	AbortChatPausedWithTask = cause("chat_paused_with_task", "Task pause aborted the main Agent chat",
		"When the user paused the task, the running main Agent conversation was also cancelled. Activity records already produced are retained; this turn is not automatically replayed when the task resumes")
	AbortChatTurnFinished = cause("chat_turn_finished", "Chat turn finished and released context",
		"This conversation turn finished normally and the server is releasing its context resources. This is not an interruption; if it appears in an interruption message, cancellation raced with the completion event")

	// Process-level and per-run hard backstop.
	AbortShutdown = cause("shutdown", "Backend process is shutting down",
		"The backend process received SIGINT or SIGTERM and is restarting, updating, or shutting down. All running Agents are cancelled; after restart, remaining running intents are reset to open and executed again")
	AbortRunHardTimeout = cause("run_hard_timeout", "Run hard timeout triggered",
		"This run exceeded its soft wall-clock budget and extra grace period, indicating that a model request or tool did not return for a long time and prevented normal wrap-up at a turn boundary. Check the last tool call that had not returned before the interruption")
)

// AbortReason resolves the named cause attached to a cancelled run context.
func AbortReason(ctx context.Context) (code, short, text string, ok bool) {
	c := context.Cause(ctx)
	if c == nil {
		return "", "", "", false
	}
	var ac *AbortCause
	if errors.As(c, &ac) {
		return ac.Code, ac.Short, ac.Text, true
	}
	switch {
	case errors.Is(c, context.DeadlineExceeded):
		return "deadline_exceeded", "Upstream context reached its deadline",
			"The upstream context reached its deadline, but the caller did not attach a named cause through WithTimeoutCause: " + c.Error(), true
	case errors.Is(c, context.Canceled):
		return "canceled_no_cause", "Canceller supplied no named cause",
			"The upstream context was cancelled, but the canceller did not attach a named cause through context.WithCancelCause; register a cause in agent/cancelcause.go and wire it into this cancellation point", true
	default:
		return "other", firstLine(c.Error(), 80), c.Error(), true
	}
}
