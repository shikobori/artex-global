package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter defines the notification_channels.filter JSONB contract. All fields are optional; omitted fields
// mean no filtering, also the fallback for malformed configuration (see ParseFilter).
type Filter struct {
	// MinSeverity is the minimum severity (low/medium/high/critical); empty means no threshold.
	MinSeverity string `json:"min_severity"`
	// Empty TaskIDs / AssetIDs arrays impose no restriction; nonempty arrays must intersect the event.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// Empty VulnClassInclude accepts all classes; otherwise vulnclass must contain an included keyword. Any
	// VulnClassExclude match takes precedence. Matching uses case-insensitive substrings rather than regex,
	// avoiding silent failure from invalid patterns.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange controls status-change notifications and applies only to realtime mode.
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter parses channel filters and deliberately never returns an error. Malformed configuration
// becomes a zero-value Filter (no filtering): an extra notification is preferable to silently losing a
// high-severity finding. Failed parsing must not create a seemingly configured channel that sends nothing.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On parse failure, f remains zero-valued and does not filter.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity reports whether s is a valid threshold; empty means no threshold.
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks restricted filter values when saving a channel. Unknown thresholds make Match compare
// rank >= 0 and silently send everything; reject typos such as "hgih" on write so users do not unknowingly
// disable filtering. Reads still use tolerant ParseFilter semantics so existing bad values cannot make an
// entire channel unreadable.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("Invalid minimum severity: %q. Choose low / medium / high / critical, or leave empty for no limit", f.MinSeverity)
	}
	return nil
}

// Match decides whether an event should reach this channel. Like ParseFilter, it never returns an error and
// treats internal failures as matches. Checks proceed through event type, severity, task/asset scope, and
// vulnerability keywords.
func Match(f Filter, s Snapshot) bool {
	// Status-change events require explicit opt-in. Users generally expect notifications about new findings
	// rather than every status transition.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclusion takes precedence even when an inclusion keyword also matches.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// Linear scans suffice for these small, manually selected sets of a few dozen items; building a map costs
	// more than it saves.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold reports whether s contains any keyword, ignoring case.
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
