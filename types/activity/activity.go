package activity

import "time"

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Type string

const (
	TypeImagePull         Type = "image_pull"
	TypeImageBuild        Type = "image_build"
	TypeImageUpdateCheck  Type = "image_update_check"
	TypeProjectPull       Type = "project_pull"
	TypeProjectBuild      Type = "project_build"
	TypeProjectDeploy     Type = "project_deploy"
	TypeProjectRedeploy   Type = "project_redeploy"
	TypeProjectDown       Type = "project_down"
	TypeProjectRestart    Type = "project_restart"
	TypeProjectDestroy    Type = "project_destroy"
	TypeContainerStart    Type = "container_start"
	TypeContainerStop     Type = "container_stop"
	TypeContainerRestart  Type = "container_restart"
	TypeContainerRedeploy Type = "container_redeploy"
	TypeContainerDelete   Type = "container_delete"
	TypeVulnerabilityScan Type = "vulnerability_scan"
	TypeAutoUpdate        Type = "auto_update"
	TypeSystemPrune       Type = "system_prune"
	TypeResourceAction    Type = "resource_action"
)

type MessageLevel string

const (
	MessageLevelInfo    MessageLevel = "info"
	MessageLevelWarning MessageLevel = "warning"
	MessageLevelError   MessageLevel = "error"
	MessageLevelSuccess MessageLevel = "success"
)

type Activity struct {
	ID                    string         `json:"id"`
	EnvironmentID         string         `json:"environmentId"`
	BatchID               *string        `json:"batchId,omitempty"`
	SourceEnvironmentID   string         `json:"sourceEnvironmentId,omitempty"`
	SourceEnvironmentName string         `json:"sourceEnvironmentName,omitempty"`
	Type                  Type           `json:"type"`
	Status                Status         `json:"status"`
	ResourceType          *string        `json:"resourceType,omitempty"`
	ResourceID            *string        `json:"resourceId,omitempty"`
	ResourceName          *string        `json:"resourceName,omitempty"`
	Progress              *int           `json:"progress,omitempty"`
	Step                  string         `json:"step,omitempty"`
	LatestMessage         string         `json:"latestMessage,omitempty"`
	StartedBy             *StartedBy     `json:"startedBy,omitempty"`
	StartedAt             time.Time      `json:"startedAt"`
	EndedAt               *time.Time     `json:"endedAt,omitempty"`
	DurationMs            *int64         `json:"durationMs,omitempty"`
	Error                 *string        `json:"error,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
	CreatedAt             time.Time      `json:"createdAt"`
	UpdatedAt             *time.Time     `json:"updatedAt,omitempty"`
}

type StartedBy struct {
	UserID      string `json:"userId,omitempty"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
}

type Message struct {
	ID         string         `json:"id"`
	ActivityID string         `json:"activityId"`
	Level      MessageLevel   `json:"level"`
	Message    string         `json:"message"`
	Payload    map[string]any `json:"payload,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type Detail struct {
	Activity Activity  `json:"activity"`
	Messages []Message `json:"messages"`
}

type StreamEvent struct {
	Type          string     `json:"type"`
	EnvironmentID string     `json:"environmentId,omitempty"`
	ActivityID    string     `json:"activityId,omitempty"`
	Activity      *Activity  `json:"activity,omitempty"`
	Activities    []Activity `json:"activities,omitempty"`
	Message       *Message   `json:"message,omitempty"`
	Error         string     `json:"error,omitempty"`
	Timestamp     time.Time  `json:"timestamp"`
}

type ClearHistoryResult struct {
	Deleted int64 `json:"deleted"`
}
