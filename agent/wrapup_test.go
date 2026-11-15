package agent

import (
	"strings"
	"testing"
	"unicode"
)

// Wrap-up/settlement prompts are injected when a run or task reaches its step or
// time budget and directly instruct the final user-visible summary. The defaults
// must be in English, preserve tool names, and retain instructions such as
// summarizing in one sentence of plain text.
//
// DB seeds leave wrapup_prompt / task_timeout_wrapup_prompt empty (the built-in
// agent INSERT in db/db.go does not fill them). Empty values fall back to these
// constants, making them the single source of default wrap-up wording.
func TestWrapupPromptsUseEnglish(t *testing.T) {
	all := map[string]string{
		"settleWrapUpPrompt":        settleWrapUpPrompt,
		"plannerWrapUpDefault":      plannerWrapUpDefault,
		"mainAgentWrapUpDefault":    mainAgentWrapUpDefault,
		"genericWrapUpDefault":      genericWrapUpDefault,
		"workerTaskTimeoutDefault":  workerTaskTimeoutDefault,
		"plannerTaskTimeoutDefault": plannerTaskTimeoutDefault,
	}

	hasScript := func(s string, table *unicode.RangeTable) bool {
		for _, r := range s {
			if unicode.Is(table, r) {
				return true
			}
		}
		return false
	}

	for name, p := range all {
		if hasScript(p, unicode.Hangul) || hasScript(p, unicode.Han) {
			t.Errorf("%s: default prompt contains untranslated text: %q", name, p)
		}
	}

	// Tool names are identifiers and must remain unchanged. Both Worker prompts
	// record conclusions with record_fact, findings with report_finding, and end
	// with a one-sentence summary in plain text.
	mustContain := func(name, p string, subs ...string) {
		for _, s := range subs {
			if !strings.Contains(p, s) {
				t.Errorf("%s: required instruction %q was not preserved", name, s)
			}
		}
	}
	mustContain("settleWrapUpPrompt", settleWrapUpPrompt,
		"insert_assets", "record_fact", "report_finding", "one sentence", "plain text")
	mustContain("workerTaskTimeoutDefault", workerTaskTimeoutDefault,
		"insert_assets", "record_fact", "report_finding", "one sentence", "plain text")
	mustContain("genericWrapUpDefault", genericWrapUpDefault, "one sentence", "plain text")
	mustContain("mainAgentWrapUpDefault", mainAgentWrapUpDefault, "one sentence", "plain text")
	// Planner does not write a summary (it only assesses and exits), and retains intent/goal/todo tools.
	mustContain("plannerWrapUpDefault", plannerWrapUpDefault, "add_intent", "prove_goal", "TodoWrite")
	mustContain("plannerTaskTimeoutDefault", plannerTaskTimeoutDefault, "prove_goal")
}

// Per-run and task-timeout prompts must differ, especially for Planner: per-run
// ends only this round, while task-timeout ends the whole task. Their mappings
// must never be mixed up.
func TestWrapupDefaultsRouting(t *testing.T) {
	if WrapupDefault("worker") != settleWrapUpPrompt {
		t.Error("worker per-run default is not settleWrapUpPrompt")
	}
	if WrapupDefault("planner") != plannerWrapUpDefault {
		t.Error("planner per-run default is not plannerWrapUpDefault")
	}
	if WrapupDefault("mainagent") != mainAgentWrapUpDefault {
		t.Error("mainagent per-run default is not mainAgentWrapUpDefault")
	}
	// Unknown keys (custom agents) fall back to generic.
	if WrapupDefault("unknown-agent") != genericWrapUpDefault {
		t.Error("Unknown key does not fall back to genericWrapUpDefault")
	}
	// Only worker/planner have task-timeout prompts; other keys return empty (callers fall back to per-run).
	if TaskTimeoutWrapupDefault("worker") != workerTaskTimeoutDefault {
		t.Error("worker task-timeout default is not workerTaskTimeoutDefault")
	}
	if TaskTimeoutWrapupDefault("planner") != plannerTaskTimeoutDefault {
		t.Error("planner task-timeout default is not plannerTaskTimeoutDefault")
	}
	if TaskTimeoutWrapupDefault("mainagent") != "" {
		t.Error("mainagent must have no task-timeout prompt (empty string)")
	}
	// Identical per-run and task-timeout prompts would erase their semantic distinction.
	if workerTaskTimeoutDefault == settleWrapUpPrompt {
		t.Error("worker per-run and task-timeout prompts are identical")
	}
	if plannerTaskTimeoutDefault == plannerWrapUpDefault {
		t.Error("planner per-run and task-timeout prompts are identical")
	}
}
