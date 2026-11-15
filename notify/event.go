package notify

// Snapshot is the contract for notification_events.snapshot (JSONB). The db finding transaction writes
// it; the server delivery engine and filters read it. This notification-domain payload lives here
// because db only serializes it. Snapshot finding fields instead of querying at render time so later
// title, severity, or status edits cannot misrepresent the original event. This also avoids joining
// findings/tasks/assets during fan-out and rendering.
type Snapshot struct {
	// Event kind: finding_created / finding_status_changed.
	Kind      string  `json:"kind"`
	FindingID int64   `json:"finding_id"`
	TaskID    int64   `json:"task_id"`
	VulnClass string  `json:"vulnclass"`
	Name      string  `json:"name"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
	AssetIDs  []int64 `json:"asset_ids"`
	// Nonempty only for kind=finding_status_changed.
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Item is a finding awaiting delivery, used by channel renderers.
type Item struct {
	FindingID int64
	Name      string
	VulnClass string
	Severity  string
	Summary   string
	// Assets contains resolved display names (e.g. domains/IPs), populated by the server because this
	// package does not access the database.
	Assets []string
	// DetailURL links to the finding; omit it when public_base_url is not configured.
	DetailURL string
	// For status change events; render the transition, such as Pending to Fixed.
	FromStatus string
	ToStatus   string
}

// IsStatusChange reports whether this item is a status change event.
func (i Item) IsStatusChange() bool { return i.FromStatus != "" || i.ToStatus != "" }

// Title prefers the user-provided name, then vulnclass, then a placeholder; never return an empty
// title.
func (i Item) Title() string {
	if i.Name != "" {
		return i.Name
	}
	if i.VulnClass != "" {
		return i.VulnClass
	}
	return "(Unnamed vulnerability)"
}

// Message contains the complete content for one channel send.
type Message struct {
	// One item for immediate delivery, or a full batch for a digest. Callers must supply at least one
	// item.
	Items []Item
	// Batch=true renders a digest with a different title, time window, and count.
	Batch bool
	// WindowMinutes is the digest interval, used only in batch text. Pass it explicitly from configuration
	// rather than computing time.Since while rendering to keep output deterministic and testable.
	WindowMinutes int
	// HomeURL is the platform dashboard address (global public_base_url); omit the dashboard link when
	// empty.
	HomeURL string
}
