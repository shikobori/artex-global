package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// goalsDefaultTmpl is the built-in EDITABLE body (section [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You are a penetration-testing goal decomposer. Your responsibility is to identify the **final outcomes to achieve** from the user's input, not to plan attack steps.

**First step (before decomposing goals): extract operational constraints**
Identify the operator's explicit rules about [allowed and prohibited operations] in the "task goal / task description", and register each using set_constraints (skip extraction if neither includes operational constraints):
- type=deny: prohibited operations (e.g., "no port scans", "no writes/deletes in production", "no brute force", "do not touch a particular subdomain").
- type=allow: explicitly allowed or limited operational scope (e.g., "passive reconnaissance only", "only this domain").
- A constraint is neither a goal nor an attack step: it defines the boundaries of operations.
- **Constraints must be [self-contained and specify concrete targets]**: replace **references** such as "current target/current port/current IP/current domain/this site" with **concrete values** from the task goal/description. Constraints are injected separately into execution prompts, where such references cannot be resolved without their original context.
  Example: for https://abc.example.net, write "Only test abc.example.net", not "Only test the current target"; write "Only test target port 443; do not scan other ports", not "Only test the current port". If the text says only "current target" but its address is known, include that address.
- **Register only constraints [explicitly stated or emphasized] in the goal/description; never invent them**. If the type is uncertain, use deny (the more conservative choice).
- If the goal/description truly has no operational constraints, **do not** call set_constraints.
After registering constraints (if any), proceed to goal decomposition below.

**Goal = a final deliverable or verifiable outcome**

**These are not goals (do not list them as subgoals)**:
- Information gathering, reconnaissance, endpoint scanning
- Vulnerability analysis and validation processes
- Attack steps or exploitation methods
- Result-verification steps

**Decomposition principles**:
- If the user describes one final goal, output one.
- If there are multiple **independent** final deliverables, list each separately.
- Set vulnclass when a clear vulnerability class applies; leave it empty for information-gathering/business-logic goals.
- Never invent goals the user did not mention.

Call set_goals to submit the results.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

**Additional responsibility: register the testing asset scope**
Alongside decomposing goals, identify the **explicitly specified testing asset scope** in the "task goal / task description", and register it using add_task_scope (this is the task's authorization boundary and the denominator for asset testing coverage). **Minimum-scope principle: register only the specific target the user explicitly named; never expand it on your own.**
- If the target is a URL or an address with a hostname (e.g., https://xxx.example.com/path or app.example.com), use its **complete hostname**, kind=subdomain, value=the complete hostname.
  Example: https://a1b2c3.lab.example.net/path -> kind=subdomain, value=a1b2c3.lab.example.net (**not** example.net).
  **Never** reduce a hostname containing a subdomain to its root domain: registering all of example.com for xxx.example.com exceeds the user's target and violates minimum scope.
- Use kind=root_domain, value=example.com **only** when the user specifies a **bare root domain with no subdomain** (e.g., example.com directly), or explicitly says "entire site / all subdomains / entire domain".
- A plain IP or network range -> kind=ip / cidr, value=the IP or CIDR.
- **Do not** register company scope (company): the task has just been created, and the company is usually not yet in the asset system, so registration would fail. Leave company-level scope to the subsequent plan phase.
Other rules:
- Register only scope **explicitly stated in the goal/description**; never invent or infer unmentioned domains/IPs.
- In reason, briefly identify the source sentence for auditing.
- If the goal/description contains no explicit asset scope, **do not** call add_task_scope.
First register scope with add_task_scope (if any), then call set_goals to submit goals.`

// goalsSystem assembles the goals-decomposer system prompt: the rendered body
// [A] (DB-overridable), the code-owned scope-extraction tail when add_task_scope
// is wired (withScope), and the code-owned output-language tail [C] last —
// mirroring chatSystem/plannerSystem so a DB-edited body can never drop the tail.
// DecomposeGoalsWithProvider and the localization test share this one assembly, so
// the langDirective tail can't drift between runtime and test. EngagementDescription
// is intentionally left empty: the task description rides in the user message, not
// the {{.EngagementDescription}} var (see DecomposeGoalsWithProvider).
func goalsSystem(dataDir string, withScope bool) string {
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{DataDir: dataDir, Now: nowStr()})
	if withScope {
		sys += goalsScopeTail
	}
	return sys + langDirective(resolveOutputLang())
}

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// prov is supplied by the caller (rather than built here from a Config) so goal
// decomposition rides the SAME provider instance as the rest of the engine — it
// shares the rate limiter, gets recorded by llmrec, and participates in LLM
// failover instead of quietly bypassing all three.
//
// desc is the task's free-text description (background: target scope/flag count/engagement instructions, etc.).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
//
// as + taskID, when non-nil/positive, wire the add_task_scope tool so the
// decomposer can register the explicit asset scope it extracts from the goal.
//
// ts is the task's exploration store: set_goals writes the decomposed goal nodes
// straight into it (the same managed tool the main agent uses to add goals at
// runtime). The returned specs are read back from the store so callers can emit
// per-goal activity and detect the "LLM produced nothing" case for their fallback.
func DecomposeGoals(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	return DecomposeGoalsWithProvider(ctx, prov, dataDir, goalText, desc, as, ts, taskID, false, 0, emit)
}

// DecomposeGoalsWithProvider is the task-runtime variant used when a task has an
// ordered provider chain. It preserves the same tools and write behavior while
// letting the caller own provider selection/failover. maxTokens is the profile's
// per-reply output cap (0 = send none).
func DecomposeGoalsWithProvider(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, nonStreaming bool, maxTokens int, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	// Goal decomposition is a one-shot call without a transcript store, so agentcore does not add
	// a session id to ctx (it does so only with a writer; see agentcore.Prompt). Gateways using
	// session-id headers for prompt caching/sticky routing read this ctx value (opencode zen returns
	// 400 MissingSessionID without x-opencode-session). Without it, chat works but decomposition gets 400.
	// Explicitly attach a stable id shared by decomposition requests in the same exploration for caching,
	// with a name distinct from planner/worker that llmrec.parseSession can attribute correctly.
	if ts != nil {
		ctx = transcript.WithSessionID(ctx, fmt.Sprintf("exp%d-goals", ts.ID()))
	}
	// worker="goals" tags the goal nodes' provenance; ts/taskID let set_goals link
	// each goal under the task root. This is the catalog's real set_goals tool, so a
	// web-edited description/schema on it applies here too.
	tsx := &ToolSet{as: as, ts: ts, taskID: taskID, worker: "goals"}
	// Wire add_task_scope only when we have a real asset store + task to write to.
	// goalsSystem appends the scope-extraction tail in lockstep (withScope) so the
	// prompt never asks for a tool that isn't present, and it owns the output-language
	// tail last so a DB-edited body can't drop it. Description rides in the user
	// message, NOT the {{.EngagementDescription}} var, so a prompt can't inject it twice.
	withScope := as != nil && taskID > 0
	sys := goalsSystem(dataDir, withScope)
	// set_constraints is always available (independent of the asset store). The editable body already says
	// to extract operational constraints before goals; only the tool needs wiring here.
	tools := []actool.CoreTool{tsx.setGoals(), tsx.setConstraints()}
	if withScope {
		tools = append(tools, tsx.addTaskScope())
	}
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background, possibly including target scope/flag count/engagement instructions; for reference only, do not invent unmentioned details):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  tools,
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		// Each of the three steps (extract constraints -> register scope -> decompose goals) needs a tool call; allow enough turns to reach set_goals before wrap-up.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // Use Provider.Complete when this profile selects non-streaming.
		MaxTokens:    maxTokens,    // 0 = send no cap; use the server default.
	}, userMsg, captureEmit)
	// set_goals persisted the goals directly; read them back so the caller sees what
	// was written (empty slice ⇒ the LLM produced nothing ⇒ caller falls back).
	if ts == nil {
		return nil
	}
	nodes, _ := ts.ListByKind(db.KindGoal, 10000)
	var out []GoalSpec
	for _, n := range nodes {
		var p struct {
			Text      string `json:"text"`
			VulnClass string `json:"vulnclass"`
		}
		_ = json.Unmarshal(n.Payload, &p)
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, GoalSpec{Text: p.Text, VulnClass: p.VulnClass})
		}
	}
	return out
}
