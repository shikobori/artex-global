package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Your planning todos (preserved across wake-ups, written by you last round)]:\n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("Advance accordingly: dispatch the next step only when [prerequisites are complete / required facts exist]. Update the list with TodoWrite, marking steps satisfied by facts completed. Do not redispatch steps already pending/in_progress on the list.")
	return b.String()
}

// TriggerEvent describes what concretely caused this planning round to fire, so
// the planner looks first at the actual change instead of re-scanning the whole
// overview. Kind:
//
//	"done"    — a worker finished intent IntentID (its output conclusion is fetched).
//	"finding" — a worker reported a finding on intent IntentID (Detail = summary).
//	"goal"    — the human (via the main agent's set_goals) added one OR MORE goals in a
//	            single call (Goals = newly added goal texts, 1+ entries; set_goals supports batches).
//	"goal_deleted" — the human deleted a goal from overview goal management (Detail = deleted goal text).
//	"goal_edited"  — the human edited a goal from overview goal management (OldGoal→NewGoal text).
//	"cancelled" — the human deleted intent IntentID (Detail = deletion reason). The intent is
//	            stopped (not deleted) and the reason is attached to it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // Kind=="cancelled" only: intent summary captured before deletion, after which the node cannot be queried.
	Goals    []string // Kind=="goal" only: one or more goal texts added by this set_goals call.
	OldGoal  string   // Kind=="goal_edited" only: goal text before editing.
	NewGoal  string   // Kind=="goal_edited" only: goal text after editing.
	Hints    []string // Kind=="hint" only: one or more hint texts added by this add_hint call.
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Actual changes that triggered this round (read these first, then decide whether to add directions)]:")
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added a goal: %s. This is a new goal to achieve; add exploration directions if no corresponding intent exists.", ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added %d goals: %s. Each is a new goal to achieve; add exploration directions for each without a corresponding intent.", len(ev.Goals), strings.Join(ev.Goals, "; ")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added a strategic hint: %s. It is attached to the exploration graph; adjust/add exploration directions if no corresponding intent exists.", ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added %d strategic hints: %s. All are attached to the exploration graph; adjust/add exploration directions for each.", len(ev.Hints), strings.Join(ev.Hints, "; ")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf("\n- The human deleted this goal: %s. It has been removed; reassess the remaining goals/directions and stop dispatching intents for it.", ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf("\n- The human changed a goal from [%s] to [%s]. Adjust exploration directions for the new goal and stop dispatching old directions that no longer apply.", ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) reported a finding: %s", ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// Prefer Summary captured at deletion; the node no longer exists for intentSummary to query.
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf("\n- The user deleted intent #%d, whose content was: %s, for this reason: %s. The intent has been deleted and will no longer execute; replan accordingly.", ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) finished with this conclusion: %s", ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf("; ids of facts newly produced by this intent: %s ", fids))
			}
		}
	}
	b.WriteString("\n(Retrieve full details with node_detail / get_worker_output / list_findings.)")
	return b.String()
}

// factIDsYielded lists the fact ids an intent produced this run as "#12, #15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, ", ")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return "(failed to fetch output)"
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return "(this work has no output record yet)"
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " … (truncated; see get_worker_output for the full text)"
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\n[Situation this round (prefetched graph_overview, identical to calling the tool; use node_detail/list_facts etc. for details as needed)]:\n" + string(b)
}

// plannerDefaultTmpl is the built-in EDITABLE body (section [A]) of the planner prompt,
// seeded into agent_prompts. Goal is a {{.Goal}} template var; the artifact-output rules
// tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = `You are the "planner" of an authorized penetration-testing system on a cybersecurity platform, awakened frequently whenever the graph changes. Your responsibility: read the situation -> assess goals -> add exploration intents **only when a genuinely new direction is not already covered**. You plan rather than execute: all outputs this round must be [generating/clarifying intents] or [assessing goals]; never carry out the work in plan.

Task goal: {{.Goal}}

**How many intents to produce this round (consider this first)**:
- **Hard minimum (highest priority)**: whenever [the goal is unmet] and [there are no open or running intents] (frontier_open=0 and running_intents is empty), you [must] produce at least one intent that advances the goal this round. With no running work to wait for and no queued direction, zero intents means the task stalls. Even if all known directions appear only in recent_done, open a new direction or continue one according to the done/exhausted/blocked rules below.
- Apart from this minimum, **zero intents is a normal outcome, but requires a valid reason** (not a default assumption that fewer dispatches are safer): (1) **Already covered**: every direction you considered is handled by an open/running intent (rephrasing an existing intent into a duplicate is a serious error). (2) **Waiting for a dependency**: the next step needs an output from running work that is not available yet (forcing a dispatch now leaves downstream work idle without prerequisites; wait for the next wake-up and graph update).
- Conversely, dispatch a genuinely new direction that is [uncovered and independent of running work], or an untested in-scope surface while the goal remains unmet. Do not use zero intents as a lazy default.

**Decision process on every wake-up**:

1. **The complete situation is appended below** (the graph_overview result, so no need to call it): task (original title + goal/root node), asset counts, goals + statuses, open/running/recent_done intents, sites_without_endpoints (sites without endpoints, suggesting unexplored directions), facts (exploration fact count, distinct from vulnerabilities), and recent_facts ({id,summary,confidence?}).
   - **Scope**: exploration nodes (goals/intents/facts/findings) contain only this task; **the asset graph is shared globally** (one graph across tasks; counts cover global in-scope assets, not exclusively this task). Ignore assets unrelated to this task.
   - **Lineage**: each intent has parents (upstream facts/intents it derives from) and yields (downstream facts/findings it produced); each recent_facts entry has from_intent. Use these to understand which facts came from which direction and whether they imply new directions.
   - **Negative/uncertain observations** (e.g., "port closed/not injectable" in recent_facts) are worker observations, not final conclusions. Before accepting one, inspect evidence with node_detail(id). Treat a direction as temporarily closed only with solid evidence, confidence=observed, and exhausted methods. Missing evidence, a mere impression/single probe, or confidence=inferred means [not yet established]. If in scope and not covered by another intent, dispatch one recheck by default to confirm or refute it (**at most one recheck for the same negative direction**; if it remains negative with reasonable evidence, respect that conclusion and stop dispatching it).
   - **Request deeper details only as needed**: list_facts (paginated, newest first, default 20; q filter, before pagination, total/has_more), list_findings (all vulnerabilities), node_detail(id) (full evidence/details; lists/recent_facts provide summaries only), list_assets (pull: q search, type/company_id/task_id filters, pagination, or direct id/ids lookup), asset_neighbors. Assets are globally shared; do not fetch everything by default.

2. **Assess goals (core responsibility)**: goals already contains objectives and statuses. For an unmet goal proved by a finding/fact, call prove_goal(goal_id, evidence_id, reason) to mark it met. **Marking the last unfinished goal automatically completes the entire task**. Completion is driven only by individual prove_goal calls, with no other one-click completion mechanism.
   - **Check quantitative acceptance criteria (never approve prematurely)**: when a goal includes measurable requirements (X% coverage, N flags, certain privileges), you **must** check the measured graph_overview values above (coverage.pct, findings_total, etc.) before prove_goal. If unmet, **do not** call prove_goal; dispatch intents to close the gap. Do not mark met early because it is "mostly achieved/the core is done". Example: required coverage 100%, measured coverage.pct=40% -> unmet; dispatch more testing intents.

3. **(Optional, startup only, extremely lightweight) Probe for understanding**: only when the graph has almost no facts (recent_facts essentially empty, task just started) and the situation alone cannot make the initial intent concrete, use Bash or similar for a very small number of read-only target probes (e.g., 1–2 curl requests for the homepage/fingerprint). **The only legitimate output is a more precise one-sentence intent description**, never vulnerability discovery/validation/exploitation or endpoint/directory/parameter enumeration (those belong to workers; dispatch them as intents). Three hard boundaries:
   - If worker facts already exist (facts>0 / recent_facts nonempty), **do not** probe yourself. Base judgments on existing facts; this round may only dispatch new intents or finish. To investigate a lead, dispatch a worker intent instead of running curl yourself.
   - Even at startup, stop after at most 3 probes, solely to clarify the initial intent. If you find yourself investigating deeply instead of quickly choosing a direction (enumerating endpoints/directories, trying ids, decoding chains, repeatedly probing the same API, any injection/authorization-bypass/vulnerability testing), stop immediately and turn it into an intent. These are worker tasks.
   - If existing facts/situation suffice, no probe is needed.

4. **Choose new directions to add**: **restraint means [avoiding duplicate existing intents], not minimizing dispatches**. While the goal is unmet, ask what deeper, stronger, uncovered approach can advance it, rather than whether to wrap up. Intents are [open exploration directions], not fixed categories or a menu. Choose from known facts, assets, and goals, comparing each against open + running + recent_done:
   - Covered by open/running -> do not generate; it is being handled.
   - Appeared in recent_done -> **first inspect its state (included on each item) to understand why it stopped**:
     * **done (finished normally)**: covered, so do not redispatch unchanged. Judge whether it is a dead end from yielded facts, not state. Redispatch only with a **materially new mechanism** (new facts/assets/parameters or a clearly different approach), and explain the difference in summary. Rewording or "maybe another try will work" does not qualify; do not retry.
     * **exhausted (budget depleted, interrupted halfway with partial write-back) / blocked (model/network failure, little exploration completed)**: both ended prematurely with incomplete information. First use get_worker_trace / get_worker_output to see actual progress and obstacles, then choose: near a breakthrough when budget expired -> dispatch a continuation from that progress; pure external failure prevented execution (often blocked) -> redispatch the same direction; repeatedly stuck at the same point -> change approach/direction. Base this on actual trace progress, never state alone.
   - Entirely new direction with no covering intent -> generate.
   - Every known direction covered by open/running -> generate nothing and finish, waiting for running/queued work. But if only recent_done covers them, no open/running remains, and the goal is unmet, the hard minimum above requires a new or continued direction.
   - **Depth before coverage**: coverage is a minimum/acceptance criterion, not the exploration goal itself. After finding a high-value entry point (potential RCE/privilege escalation/data leakage), prioritize intents that **pursue it deeply**, rather than shallow testing across assets merely to balance coverage.
   - **Keep routes diverse; do not converge too early**: while the goal is unmet, if existing intents cluster on one route/entry point and an uncovered **fundamentally different** direction exists (another entry surface/asset class/exploit chain), prioritize that divergence rather than synonymous intents on the same route. Judge substantive differences, not wording; still do not generate if that direction is already covered. Ideally keep 2–3 mechanisms alive (e.g., upload chain and authentication bypass), concentrating resources only after one provides evidence of progress toward the goal. **Diversity always obeys [Operational constraints] above**: never generate intents for excluded surfaces/ports/hosts/operations, however different they are.

   **Serial exploit chains: dispatch step by step, not in parallel.** For a strongly dependent chain (1 -> 2 -> 3, each requiring actual output from the previous step), do not dispatch all steps in parallel: downstream work lacks prerequisites and merely repeats work or idles. Record the whole chain with TodoWrite (one item per step); dispatch only the step whose prerequisites are satisfied this round (usually the first). After its facts arrive, dispatch the next step on the next wake-up (the todo list will be supplied), marking satisfied steps completed. Do not split the same action into two intents ("confirm the trigger" and "trigger the trigger" are one step). Parallel intents are only for independent dimensions, such as enumerating unrelated endpoints.

5. **Submit**: batch the selected new directions in **one** add_intent call (intents array, at most the 4 highest-value directions; do not call separately for each):
   - **summary**: one natural-language sentence describing the direction (complete target address + what to do + why), without a fixed category. Deduplication primarily compares this with existing intents.
   - **asset_ids**: ids of target assets to test/attack (provide whenever possible; zero, one, or several from list_assets). Always provide them when the direction concerns concrete assets (site/API/parameter/host), for coverage deduplication and asset linkage; include all relevant assets for a cross-asset direction. Leave empty only for purely global reconnaissance with no concrete asset.
   - **parent_ids**: upstream nodes from which this direction was synthesized (optional; zero, one, or several). Include all facts combined into the intent, or the id of an upstream intent/finding it derives from; leave empty for an entirely new top-level direction.

Avoid duplicates and forced filler, but dispatch uncovered, deeper approaches when the goal remains unmet. Be concise, focused, and efficient.`

func plannerSystem(goal, dataDir, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir) + langDirective(resolveOutputLang())
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner")
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// Domain tools + the default base tools (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash).
	// Drop add_task_scope/list_untested_assets from the prompt when asset coverage is disabled.
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// Put key situational context (just-completed intents + prefetched graph) in this round's user input
	// below, leaving only static planning text in system for stability and caching. Long rounds may have
	// this context compacted, but planner rounds are usually short, so risk is low. Append situational below.
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// Task deadline/final mode comes from ctx (see taskclock.go). In the final round, append the planner
	// task-timeout wrap-up prompt as this round's operating instructions in user input with situational,
	// so it only makes final goal assessments and creates no new intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\n[Final task wrap-up (special instructions for this round, overriding the normal planning process above)]:" + resolveTaskTimeoutWrapup("planner")
	}
	// Create this task's working directory <workDir>/tasks/<taskID> first.
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir)
	if p.wantConstraints() {
		sysBody += constraintBlock(ts) // Inject operational constraints, if any, into system to bound exploration.
	}
	system, boundary := deferredSystem(sysBody, def)
	// The planner has no wall-clock budget of its own. With a deadline, clamp MaxDuration to time remaining
	// so the active round wraps up at expiry (timeout -> task-timeout prompt; steps -> per-run prompt).
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // Use the recording proxy; load its CA to verify MITM-reissued HTTPS certificates.
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Optional web search: ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate outbound proxy (http/https/socks5), unrelated to the recording MITM proxy; empty means direct.
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Bash subprocesses use the proxy and trust its CA by default.
		WorkingDir:            taskDir,                              // This task's working directory <workDir>/tasks/<taskID>.
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // 0 = unlimited; with a deadline, the time remaining.
		Compaction:            compactionConfig(p.compactionWindow()),
		// Share planning todos across wake-ups so serial chains survive rounds (new session, same store).
		Todos: p.todoFor(ts.ID()),
		// When this round's step budget is reached, the SDK wraps up by committing settled conclusions:
		// add_intent for dispatches, prove_goal for evidence, TodoWrite for serial chains. Planning resumes on future wake-ups.
		// When clamped by the task deadline, use PromptByReason (see wrapupSettlementForTask).
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // Use Provider.Complete when this profile selects non-streaming.
		MaxTokens:    p.maxTokens(),    // 0 = send no cap; use the server default.
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// Experimental: noa takes over context compaction when enabled (persistent archives under <workDir>/noa/<SessionID>).
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// Situational context (completed intents + full graph) now joins this round's user input below, alongside
	// instructions and persistent todos (regenerable model planning notes, suitable for user input).
	// Choose the opening by whether concrete changes exist: if so, point to the actual-changes block;
	// otherwise (heartbeat / hint / resume), do not claim the graph changed; ask to review running intents.
	lead := "Concrete changes just occurred (see [Actual changes that triggered this round] below). Plan the next step accordingly:"
	if len(triggers) == 0 {
		lead = "This wake-up is a **scheduled check (heartbeat) / no concrete change signal**; the graph may not have changed. Also review running intents: use steer_work to correct prolonged lack of progress or drift, and kill_work to stop entirely wrong directions. Then assess goals and decide whether to add directions:"
		// On a heartbeat/no-change wake-up, no open/running intents means exploration has stalled:
		// no active workers or queued directions. Explicitly require a new direction rather than an idle review round.
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = "This is a **scheduled check (heartbeat)** wake-up, and **no open or running intents remain**. No workers are running and no directions are queued, so exploration has stalled. You **must** produce one or more new intents this round that advance the goal and **do not duplicate** existing graph intents (zero intents is not allowed). First assess whether the goal is met from the situation below; if not, immediately add directions:"
		}
	}
	input := lead + situational + "\n\nAssess goals from the situation above. Mark each with prove_goal only when [truly achieved] (the target outcome obtained / target vulnerability confirmed). **Hard minimum: if the goal remains unmet and no open or running intents exist (frontier_open=0 and running_intents is empty), you must produce at least one intent advancing the goal this round. There is no running work to wait for or queued direction; zero intents means the task stalls. Only when open/running intents are progressing or the goal is met may this round produce no new intents.**" +
		renderPlannerTodos(opts.Todos.List())
	// MaxDuration interrupts running tools at the wall-clock limit and wraps up using the live ctx. A stuck
	// round no longer bypasses wrap-up, so no external hard ctx limit is needed; ctx handles pause/kill/shutdown.
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}
