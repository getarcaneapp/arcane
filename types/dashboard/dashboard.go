package dashboard

type ActionItemKind string

const (
	ActionItemKindStoppedContainers         ActionItemKind = "stopped_containers"
	ActionItemKindImageUpdates              ActionItemKind = "image_updates"
	ActionItemKindActionableVulnerabilities ActionItemKind = "actionable_vulnerabilities"
	ActionItemKindExpiringKeys              ActionItemKind = "expiring_keys"
)

type ActionItemSeverity string

const (
	ActionItemSeverityWarning  ActionItemSeverity = "warning"
	ActionItemSeverityCritical ActionItemSeverity = "critical"
)

type ActionItem struct {
	// Kind identifies the type of dashboard action item.
	//
	// Required: true
	Kind ActionItemKind `json:"kind"`

	// Count is the number of impacted resources for this action item.
	//
	// Required: true
	Count int `json:"count"`

	// Severity indicates urgency for the action item.
	//
	// Required: true
	Severity ActionItemSeverity `json:"severity"`
}

type ActionItems struct {
	// Items is the list of action items requiring attention.
	//
	// Required: true
	Items []ActionItem `json:"items"`
}
