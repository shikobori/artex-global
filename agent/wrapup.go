package agent

import (
	"strings"

	"github.com/Autumn-27/norma/harness"
)

// Wrap-up / settlement prompts: when an agent stops because its step budget (MaxTurns) is exhausted
// or its time budget (run_seconds/MaxDuration) expires, the SDK settlement phase injects this prompt
// to persist identified but unrecorded results before producing a one-sentence summary.
//
// Each agent's prompt can be overridden in admin settings (agents.wrapup_prompt); empty uses the built-in
// default here. Only the prompt body is editable; disabled tools and settlement policy remain code-owned.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in wrap-up prompts indexed by agent key. worker reuses the historically hardcoded settleWrapUpPrompt
// from worker.go; planner/mainagent have their own versions, and custom agents use the generic fallback.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults gives each agent's settlement phase its own default turn budget (overridable by a positive admin value).
// All receive 10 turns to allow enough steps for persistence. Unknown agents use genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "The step budget for this planning round is about to run out. Only [this round] is ending; the system will wake you again as the situation changes so you can continue planning. The task itself is not ending, so there is no need to finalize the entire plan here. Commit conclusions you have already reached this round so the round is not wasted, but [do not invent intents just to wrap up] (zero intents is a completely normal result for a round). (1) If you have already identified exploration directions [that should be dispatched immediately], submit them together in one add_intent call; do not leave settled decisions unsubmitted. (2) Use prove_goal to mark goals met when a finding or fact already proves achievement; do not omit them. (3) If you identified a serial exploit chain that needs separate steps, record it with TodoWrite so dispatch can continue at the next wake-up. When finished, end this round immediately without summary text."

const mainAgentWrapUpDefault = "The step budget is about to run out and this interaction is ending. Do not start any new exploration or operations. Use **only one sentence of plain text** to summarize the current progress, key conclusions, and recommended next step to the user in English."

const genericWrapUpDefault = "The budget is exhausted and this run is about to end. First record completed results that have not yet been saved, then use **only one sentence of plain text** to summarize in English what you did and the key conclusions (this sentence is displayed as the result of this run)."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        withWrapupLang(resolveWrapup(agentKey)),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// withWrapupLang appends the output-language instruction (langDirective) to the wrap-up prompt.
// Unlike system prompts, wrap-up prompts do not receive this tail automatically, so append it explicitly
// to make the final user-visible one-sentence summary follow the selected display language.
// The wrap-up prose is operational guidance for the model; the tail takes highest priority only for
// the language of user-visible text. It is harmless for planners, which produce no summary.
// Do not append it for the default language en: the system prompt's langDirective already requires
// English output, keeping the default model-input path stable across versions to avoid benchmark drift.
func withWrapupLang(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	lang := resolveOutputLang()
	if lang == "en" {
		return prompt
	}
	return prompt + langDirective(lang)
}

// ---------- Task-level timeout wrap-up prompts (see the task-level timeout and wrap-up design document) ----------
//
// These are separate from per-run prompts: per-run means this run's budget is exhausted; task timeout
// means the entire task is about to end. Their meaning often differs (especially for the planner: continue
// planning after a run versus stop planning and make final assessments at task expiry). Worker/planner only.

// WrapupTaskTimeoutOverride / …TurnsOverride: DB overrides for task-timeout prompts and turn budgets,
// wired to agents.task_timeout_wrapup_prompt / _max_turns for worker/planner only.
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "**The entire task has reached its time limit and is about to end** (the whole exploration is ending, not just this run's budget). This is the last opportunity. (1) Save [all] identified but unrecorded results. Use insert_assets for new assets, record_fact for exploration conclusions and facts, and report_finding for confirmed vulnerabilities. (2) Do not start any new commands or probes. (3) **At the very end, use only one sentence of plain text** to summarize this intent's key conclusions in English."

const plannerTaskTimeoutDefault = "**The entire task has reached its time limit and is about to end** (the whole task is ending, not just this round). Make final goal assessments based on [all] facts and findings obtained so far. Use prove_goal to mark goals met when evidence proves achievement; do not omit them. **Do not create any new intents** (intents dispatched now will no longer execute). Once assessments are complete, end immediately without summary text."

// TaskTimeoutWrapupDefault returns an agent's default task-timeout prompt for admin placeholders/reset.
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // Empty for agents without a configured prompt (mainagent/chat).
}

// resolveTaskTimeoutWrapup prefers a nonempty DB override over the built-in default. Empty means
// this agent has no task-timeout prompt (not worker/planner), so callers should use its per-run prompt.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // Use the per-run turn count by default.
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true: the task deadline limits this run; Timeout means task expiry, so use its timeout prompt;
//     MaxTurns means steps ran out first while task time remains, so use the per-run prompt.
//   - clamped=false: task expiry is distant; both reasons use per-run prompts, equivalent to wrapupSettlement.
//
// The harness chooses PromptByReason at settlement using the actual reason, avoiding a mismatch at build time.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := withWrapupLang(resolveWrapup(agentKey))
	st := &harness.Settlement{
		Prompt:        perRun, // Fallback, also used for both reasons when unclamped.
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  withWrapupLang(tt), // Task expired.
				harness.ReasonMaxTurns: perRun,             // Steps exhausted first while task time remains.
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}
