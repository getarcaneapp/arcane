package types

import (
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type DashboardActionItemKind string

const (
	DashboardActionItemKindStoppedContainers         DashboardActionItemKind = "stopped_containers"
	DashboardActionItemKindImageUpdates              DashboardActionItemKind = "image_updates"
	DashboardActionItemKindActionableVulnerabilities DashboardActionItemKind = "actionable_vulnerabilities"
	DashboardActionItemKindExpiringKeys              DashboardActionItemKind = "expiring_keys"

	DashboardActionItemSeverityWarning  DashboardActionItemSeverity = "warning"
	DashboardActionItemSeverityCritical DashboardActionItemSeverity = "critical"

	DashboardStreamErrorCodeAgentIncompatible = "agent_incompatible"
	DashboardStreamErrorCodeUnreachable       = "unreachable"
	DashboardStreamErrorCodeAuthFailed        = "auth_failed"
)

type DashboardActionItemSeverity string

type DashboardActionItem struct {
	// Kind identifies the type of dashboard action item.
	//
	// Required: true
	Kind DashboardActionItemKind `json:"kind"`

	// Count is the number of impacted resources for this action item.
	//
	// Required: true
	Count int `json:"count"`

	// Severity indicates urgency for the action item.
	//
	// Required: true
	Severity DashboardActionItemSeverity `json:"severity"`
}

type DashboardActionItems struct {
	// Items is the list of action items requiring attention.
	//
	// Required: true
	Items []DashboardActionItem `json:"items"`
}

type DashboardSnapshotSettings struct{}

type DashboardDockerSnapshot struct {
	Containers []dockercontainer.Summary `json:"containers"`
	Images     []dockerimage.Summary     `json:"images"`
	Networks   []dockernetwork.Summary   `json:"networks"`
	Volumes    *client.VolumeListResult  `json:"volumes"`
}

type DashboardSnapshotContainers struct {
	// Data is the first dashboard page of container summaries.
	//
	// Required: true
	Data []ContainerSummary `json:"data"`

	// Counts is the full-environment container status summary.
	//
	// Required: true
	Counts ContainerStatusCounts `json:"counts"`

	// Pagination describes the fixed first dashboard page.
	//
	// Required: true
	Pagination PaginationResponse `json:"pagination"`
}

type DashboardSnapshotImages struct {
	// Data is the first dashboard page of image summaries.
	//
	// Required: true
	Data []ImageSummary `json:"data"`

	// Pagination describes the fixed first dashboard page.
	//
	// Required: true
	Pagination PaginationResponse `json:"pagination"`
}

type DashboardSnapshot struct {
	// Containers is the dashboard container table payload.
	//
	// Required: true
	Containers DashboardSnapshotContainers `json:"containers"`

	// Images is the dashboard image table payload.
	//
	// Required: true
	Images DashboardSnapshotImages `json:"images"`

	// ImageUsageCounts is the dashboard image usage summary.
	//
	// Required: true
	ImageUsageCounts ImageUsageCounts `json:"imageUsageCounts"`

	// VolumeUsageCounts is the dashboard volume usage summary. It is a pointer
	// so agents that predate the field are distinguishable from environments
	// that genuinely have no volumes.
	//
	// Required: false
	VolumeUsageCounts *VolumeUsageCounts `json:"volumeUsageCounts,omitempty"`

	// ActionItems is the dashboard attention summary.
	//
	// Required: true
	ActionItems DashboardActionItems `json:"actionItems"`

	// Settings is the minimal settings payload needed by the dashboard.
	//
	// Required: true
	Settings DashboardSnapshotSettings `json:"settings"`

	// VersionInfo is the environment application version metadata when available.
	//
	// Required: false
	VersionInfo *VersionInfo `json:"versionInfo,omitempty"`

	PodCount *int `json:"podCount,omitempty"`
}

type DashboardStreamEvent struct {
	Type          string             `json:"type"`
	EnvironmentID string             `json:"environmentId,omitempty"`
	Snapshot      *DashboardSnapshot `json:"snapshot,omitempty"`
	Error         string             `json:"error,omitempty"`
	ErrorCode     string             `json:"errorCode,omitempty"`
	Timestamp     time.Time          `json:"timestamp"`
}
