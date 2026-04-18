package dashboard

import (
	"github.com/getarcaneapp/arcane/types/base"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	environmenttypes "github.com/getarcaneapp/arcane/types/environment"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	versiontypes "github.com/getarcaneapp/arcane/types/version"
)

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

type SnapshotSettings struct{}

type SnapshotContainers struct {
	// Data is the first dashboard page of container summaries.
	//
	// Required: true
	Data []containertypes.Summary `json:"data"`

	// Counts is the full-environment container status summary.
	//
	// Required: true
	Counts containertypes.StatusCounts `json:"counts"`

	// Pagination describes the fixed first dashboard page.
	//
	// Required: true
	Pagination base.PaginationResponse `json:"pagination"`
}

type SnapshotImages struct {
	// Data is the first dashboard page of image summaries.
	//
	// Required: true
	Data []imagetypes.Summary `json:"data"`

	// Pagination describes the fixed first dashboard page.
	//
	// Required: true
	Pagination base.PaginationResponse `json:"pagination"`
}

type Snapshot struct {
	// Containers is the dashboard container table payload.
	//
	// Required: true
	Containers SnapshotContainers `json:"containers"`

	// Images is the dashboard image table payload.
	//
	// Required: true
	Images SnapshotImages `json:"images"`

	// ImageUsageCounts is the dashboard image usage summary.
	//
	// Required: true
	ImageUsageCounts imagetypes.UsageCounts `json:"imageUsageCounts"`

	// ActionItems is the dashboard attention summary.
	//
	// Required: true
	ActionItems ActionItems `json:"actionItems"`

	// Settings is the minimal settings payload needed by the dashboard.
	//
	// Required: true
	Settings SnapshotSettings `json:"settings"`
}

type EnvironmentSnapshotState string

const (
	EnvironmentSnapshotStateReady   EnvironmentSnapshotState = "ready"
	EnvironmentSnapshotStateSkipped EnvironmentSnapshotState = "skipped"
	EnvironmentSnapshotStateError   EnvironmentSnapshotState = "error"
)

type EnvironmentOverview struct {
	// Environment is the normalized runtime environment record.
	//
	// Required: true
	Environment environmenttypes.Environment `json:"environment"`

	// Containers is the summarized container status for the environment.
	//
	// Required: true
	Containers containertypes.StatusCounts `json:"containers"`

	// ImageUsageCounts is the summarized image usage for the environment.
	//
	// Required: true
	ImageUsageCounts imagetypes.UsageCounts `json:"imageUsageCounts"`

	// ActionItems is the environment attention summary.
	//
	// Required: true
	ActionItems ActionItems `json:"actionItems"`

	// Settings is the minimal dashboard settings payload needed for per-environment actions.
	//
	// Required: true
	Settings SnapshotSettings `json:"settings"`

	// VersionInfo is the environment application version metadata when available.
	//
	// Required: false
	VersionInfo *versiontypes.Info `json:"versionInfo,omitempty"`

	// SnapshotState indicates whether the environment snapshot was fetched.
	//
	// Required: true
	SnapshotState EnvironmentSnapshotState `json:"snapshotState"`

	// SnapshotError contains a non-fatal snapshot retrieval error.
	//
	// Required: false
	SnapshotError *string `json:"snapshotError,omitempty"`
}

type EnvironmentsSummary struct {
	// TotalEnvironments is the number of visible environments.
	//
	// Required: true
	TotalEnvironments int `json:"totalEnvironments"`

	// OnlineEnvironments is the number of online environments.
	//
	// Required: true
	OnlineEnvironments int `json:"onlineEnvironments"`

	// StandbyEnvironments is the number of standby environments.
	//
	// Required: true
	StandbyEnvironments int `json:"standbyEnvironments"`

	// OfflineEnvironments is the number of offline environments.
	//
	// Required: true
	OfflineEnvironments int `json:"offlineEnvironments"`

	// PendingEnvironments is the number of pending environments.
	//
	// Required: true
	PendingEnvironments int `json:"pendingEnvironments"`

	// ErrorEnvironments is the number of error environments.
	//
	// Required: true
	ErrorEnvironments int `json:"errorEnvironments"`

	// DisabledEnvironments is the number of disabled environments.
	//
	// Required: true
	DisabledEnvironments int `json:"disabledEnvironments"`

	// Containers is the aggregated container status counts.
	//
	// Required: true
	Containers containertypes.StatusCounts `json:"containers"`

	// ImageUsageCounts is the aggregated image usage.
	//
	// Required: true
	ImageUsageCounts imagetypes.UsageCounts `json:"imageUsageCounts"`

	// EnvironmentsWithActionItems is the number of environments with attention items.
	//
	// Required: true
	EnvironmentsWithActionItems int `json:"environmentsWithActionItems"`
}

type EnvironmentsOverview struct {
	// Summary is the aggregated fleet summary.
	//
	// Required: true
	Summary EnvironmentsSummary `json:"summary"`

	// Environments contains per-environment overview rows.
	//
	// Required: true
	Environments []EnvironmentOverview `json:"environments"`
}
