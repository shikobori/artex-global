package agent

import (
	"context"

	"github.com/Autumn-27/artex/db"
)

// FindingRecorder is injected by the host; agents never synthesize or copy
// evidence bodies themselves. Its implementation owns the atomic write.
type FindingRecorder interface {
	Record(context.Context, db.RecordFindingInput, []db.TrafficRef) (*db.RecordedFinding, error)
}

// Tool-use guidance is appended without replacing the user's editable prompt.
// It does not require capture or claim that unavailable traffic tools exist.
const findingTrafficGuidance = "\n\n**Vulnerability traffic evidence (optional)**: When reporting a vulnerability with report_finding, use traffic_refs to bind real HTTP request/response IDs in reproduction order if you have inspected them and confirmed they support the finding. Domain and time are candidate filters only; they do not establish a relationship. For TCP or other non-HTTP vulnerabilities, uncaptured traffic, or no exact match, omit this field or pass []. Preserve other verifiable evidence, such as command output and logs, in evidence, and preferably explain why no traffic was bound. Do not guess IDs or repeat probes solely to capture missing traffic."

func (t *ToolSet) SetFindingRecorder(r FindingRecorder)   { t.findingRecorder = r }
func (w *Worker) SetFindingRecorder(r FindingRecorder)    { w.findingRecorder = r }
func (p *Planner) SetFindingRecorder(r FindingRecorder)   { p.findingRecorder = r }
func (m *MainAgent) SetFindingRecorder(r FindingRecorder) { m.findingRecorder = r }
