package agent

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

// PromptOverride, if set, returns the stored system-prompt template for an agent
// key and whether one exists. The server wires it to the PG agent_prompts table.
// When nil or no override exists, agents use their built-in default prompt — so
// behavior is identical until a user edits a prompt in the UI.
var PromptOverride func(agentKey string) (string, bool)

// Prompt-variable structs — fields mirror each agent's catalog (docs §5a) so a
// user template referencing a catalog variable renders; referencing anything else
// fails template execution and falls back to the built-in default.
type PlannerVars struct{ Goal, Scope, AssetSummary, DataDir, Now string }
type WorkerVars struct{ ProxyAddr, WorkerName, DataDir, Now string }
type MainVars struct{ Goal, AssetSummary, FindingsSummary, DataDir, Now string }
type GoalsVars struct{ EngagementDescription, DataDir, Now string }

// nowStr is the server-local wall-clock string exposed as the universal {{.Now}}
// prompt variable. renderSystem runs on every agent turn/round, so this is fresh
// each run — a prompt can subtract it from a fixed start stamp to reason about
// elapsed time (e.g. a timed benchmark's "last N hours" window).
func nowStr() string { return time.Now().Format("2006-01-02 15:04:05 MST") }

// renderSystem returns the rendered system-prompt BODY (section [A]) for agentKey.
// Precedence: the DB-stored template (if any) over the built-in default template
// (def). BOTH are Go templates now — the built-in default is seeded into the DB
// verbatim, so the two paths render identically until a user edits the prompt.
// Rendering always runs (def used to be pre-substituted plain text; it is now a
// {{.Var}} template like the DB one). On any render error we fall back to the
// default template, then to the raw default string — an agent never starts with a
// half-rendered prompt. Callers append the code-owned tail (trafficTool / intermediate artifact
// output specification) AFTER this, so those cannot be edited away via the DB body.
func renderSystem(agentKey, def string, vars any) string {
	tmpl := def
	if PromptOverride != nil {
		if t, ok := PromptOverride(agentKey); ok && t != "" {
			tmpl = t
		}
	}
	if out, err := renderTmpl(tmpl, vars); err == nil {
		return out
	}
	// DB template broke (e.g. references an out-of-catalog var) → code default.
	if out, err := renderTmpl(def, vars); err == nil {
		return out
	}
	return def
}

// langDirective is the output-language tail: a code-owned segment
// appended AFTER the rendered body and the artifact/traffic tails on every
// user-facing agent role, so a DB-edited prompt body can never drop it — the same
// guarantee artifactSpec gives. It does NOT translate the agent "brain": the
// reasoning body (section [A]) is not modified here. This only constrains
// the LANGUAGE of what the agent SHOWS to the user. The English directive
// enforces the selected display language while preserving the role instructions
// and localizing the surface the user reads. Raw technical strings
// (commands, payloads, code, URLs, log/response excerpts) are explicitly kept
// verbatim so evidence and reproduction steps are not mangled by translation.
//
// Two anti-drift clauses were added after the end-to-end run (L1): live models
// leaked (1) Chinese into the planner's situation summary — mirroring the Chinese
// brain body (section [A]) — and (2) the target app/evidence language into
// report_finding's structured fields. The directive names the planner situation
// summary as a user-facing field and forbids mirroring BOTH the Chinese
// instruction language AND the target/material language in the display fields.
//
// The display language is a parameter (code = "en"/"ko"/"zh"/"es"): the directive
// stays in English but the "write in <language>" token is swapped per target. When
// the display language IS Chinese ("zh"), Chinese output is the goal, so the
// "never emit Chinese to the user" clause is replaced by one that simply affirms
// Chinese display and still forbids mirroring the target/material language.
func langDirective(code string) string {
	phrase := langPhrase[code]
	bare := langBare[code]
	if phrase == "" || bare == "" {
		phrase, bare = langPhrase[defaultOutputLang], langBare[defaultOutputLang]
	}
	if code == "zh" {
		// The display language itself is Chinese, so omit the clause forbidding Chinese output to the user.
		// Instead, affirm Chinese display while retaining the ban on mirroring source languages and preserving original excerpts.
		return fmt.Sprintf("\n\n**Output language specification (localization; highest priority; cannot be overridden by the prompt body)**: Write all natural-language text **shown to the user** in **%s**, including record_fact summary/detail, report_finding titles/descriptions/conclusions/remediation advice, the planner's situation/status summaries, the final one-sentence summary, and chat replies. However, **commands, payloads, code, file paths, URLs, parameter names, and original excerpts from logs/requests/responses must be preserved verbatim**, without translation or rewriting (especially command lines and output in evidence, to support reproduction). **Even if the target system, its pages, evidence, logs, or reference materials use English or another language, user-facing natural-language fields (titles/descriptions/conclusions/remediation advice/summaries/situation summaries) must still use %s. Do not mirror or copy the target or source language into these display fields; only the technical excerpts listed above remain unchanged.** Internal reasoning is unrestricted, but all user-visible prose must be in %s, except verbatim technical excerpts.", phrase, bare, bare)
	}
	return fmt.Sprintf("\n\n**Output language specification (localization; highest priority; cannot be overridden by the prompt body)**: Write all natural-language text **shown to the user** in **%s**, including record_fact summary/detail, report_finding titles/descriptions/conclusions/remediation advice, the planner's situation/status summaries, the final one-sentence summary, and chat replies. However, **commands, payloads, code, file paths, URLs, parameter names, and original excerpts from logs/requests/responses must be preserved verbatim**, without translation or rewriting (especially command lines and output in evidence, to support reproduction). **Even if the system/role instructions above are written in Chinese, never output Chinese to the user: the only user-facing display language is %s. Do not include Chinese sentences in user-visible prose.** **Even if the target system, its pages, evidence, logs, or reference materials use English, Chinese, or another language, user-facing natural-language fields (titles/descriptions/conclusions/remediation advice/summaries/situation summaries) must still use %s. Do not mirror or copy the target or source language into these display fields; only the technical excerpts listed above remain unchanged.** **Analysis/planning/thinking in Chinese is allowed, but it is invisible internal reasoning and must never appear as response prose. Your visible reply must use %s from its first word. Do not preface it with Chinese thoughts, explanations, or statements about what you will do first. Clarifying questions, missing-parameter notices, and explanations that you cannot continue must also use %s directly.** Internal reasoning is unrestricted, but all user-visible prose must be in %s, except verbatim technical excerpts.", phrase, bare, bare, bare, bare, bare)
}

func renderTmpl(tmpl string, vars any) (string, error) {
	t, err := template.New("p").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, vars); err != nil {
		return "", err
	}
	return b.String(), nil
}
