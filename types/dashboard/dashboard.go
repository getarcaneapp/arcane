package dashboard

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
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

type DockerSnapshot struct {
	Containers []dockercontainer.Summary `json:"containers"`
	Images     []dockerimage.Summary     `json:"images"`
	Networks   []dockernetwork.Summary   `json:"networks"`
	Volumes    *client.VolumeListResult  `json:"volumes"`
}

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

	// VersionInfo is the environment application version metadata when available.
	//
	// Required: false
	VersionInfo *versiontypes.Info `json:"versionInfo,omitempty"`
}

const (
	StreamErrorCodeAgentIncompatible = "agent_incompatible"
	StreamErrorCodeUnreachable       = "unreachable"
)

type StreamEvent struct {
	Type          string    `json:"type"`
	EnvironmentID string    `json:"environmentId,omitempty"`
	Snapshot      *Snapshot `json:"snapshot,omitempty"`
	Error         string    `json:"error,omitempty"`
	ErrorCode     string    `json:"errorCode,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}
