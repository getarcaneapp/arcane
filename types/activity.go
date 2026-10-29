package types

import (
	"time"
)

type ActivityStatus string

const (
	ActivityTypeJobRun      ActivityType   = "job_run"
	ActivityStatusQueued    ActivityStatus = "queued"
	ActivityStatusRunning   ActivityStatus = "running"
	ActivityStatusSuccess   ActivityStatus = "success"
	ActivityStatusFailed    ActivityStatus = "failed"
	ActivityStatusCancelled ActivityStatus = "cancelled"

	ActivityTypeImagePull         ActivityType = "image_pull"
	ActivityTypeImageBuild        ActivityType = "image_build"
	ActivityTypeImageUpdateCheck  ActivityType = "image_update_check"
	ActivityTypeProjectPull       ActivityType = "project_pull"
	ActivityTypeProjectBuild      ActivityType = "project_build"
	ActivityTypeProjectDeploy     ActivityType = "project_deploy"
	ActivityTypeProjectRedeploy   ActivityType = "project_redeploy"
	ActivityTypeProjectDown       ActivityType = "project_down"
	ActivityTypeProjectRestart    ActivityType = "project_restart"
	ActivityTypeProjectDestroy    ActivityType = "project_destroy"
	ActivityTypeContainerStart    ActivityType = "container_start"
	ActivityTypeContainerStop     ActivityType = "container_stop"
	ActivityTypeContainerRestart  ActivityType = "container_restart"
	ActivityTypeContainerRedeploy ActivityType = "container_redeploy"
	ActivityTypeContainerEdit     ActivityType = "container_edit"
	ActivityTypeContainerDelete   ActivityType = "container_delete"
	ActivityTypeContainerKill     ActivityType = "container_kill"
	ActivityTypeContainerPause    ActivityType = "container_pause"
	ActivityTypeContainerUnpause  ActivityType = "container_unpause"
	ActivityTypeVulnerabilityScan ActivityType = "vulnerability_scan"
	ActivityTypeImagePatch        ActivityType = "image_patch"
	ActivityTypeAutoUpdate        ActivityType = "auto_update"
	ActivityTypeSystemPrune       ActivityType = "system_prune"
	ActivityTypeResourceAction    ActivityType = "resource_action"

	ActivityMessageLevelInfo    ActivityMessageLevel = "info"
	ActivityMessageLevelWarning ActivityMessageLevel = "warning"
	ActivityMessageLevelError   ActivityMessageLevel = "error"
	ActivityMessageLevelSuccess ActivityMessageLevel = "success"
)

type ActivityType string

type ActivityMessageLevel string

type Activity struct {
	StartedAt    time.Time          `json:"startedAt"`
	CreatedAt    time.Time          `json:"createdAt"`
	BatchID      *string            `json:"batchId,omitempty"`
	ResourceType *string            `json:"resourceType,omitempty"`
	ResourceID   *string            `json:"resourceId,omitempty"`
	ResourceName *string            `json:"resourceName,omitempty"`
	Progress     *int               `json:"progress,omitempty"`
	StartedBy    *ActivityStartedBy `json:"startedBy,omitempty"`
	EndedAt      *time.Time         `json:"endedAt,omitempty"`
	DurationMs   *int64             `json:"durationMs,omitempty"`
	Error        *string            `json:"error,omitempty"`
	Metadata     map[string]any     `json:"metadata,omitempty"`
	UpdatedAt    *time.Time         `json:"updatedAt,omitempty"`
	EnvironmentTag
	ID                    string         `json:"id"`
	SourceEnvironmentID   string         `json:"sourceEnvironmentId,omitempty"`
	SourceEnvironmentName string         `json:"sourceEnvironmentName,omitempty"`
	Type                  ActivityType   `json:"type"`
	Status                ActivityStatus `json:"status"`
	Step                  string         `json:"step,omitempty"`
	LatestMessage         string         `json:"latestMessage,omitempty"`
}

type ActivityStartedBy struct {
	UserID      string `json:"userId,omitempty"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
}

type ActivityMessage struct {
	CreatedAt  time.Time            `json:"createdAt"`
	Payload    map[string]any       `json:"payload,omitempty"`
	ID         string               `json:"id"`
	ActivityID string               `json:"activityId"`
	Level      ActivityMessageLevel `json:"level"`
	Message    string               `json:"message"`
}

type ActivityDetail struct {
	Activity Activity          `json:"activity"`
	Messages []ActivityMessage `json:"messages"`
}

type ActivityStreamEvent struct {
	Timestamp     time.Time        `json:"timestamp"`
	Activity      *Activity        `json:"activity,omitempty"`
	Message       *ActivityMessage `json:"message,omitempty"`
	Type          string           `json:"type"`
	EnvironmentID string           `json:"environmentId,omitempty"`
	ActivityID    string           `json:"activityId,omitempty"`
	Error         string           `json:"error,omitempty"`
	ActivityType  ActivityType     `json:"-"`
	Activities    []Activity       `json:"activities,omitempty"`
}

type ActivityClearHistoryResult struct {
	Deleted int64 `json:"deleted"`
}
