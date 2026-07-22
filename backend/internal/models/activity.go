package models

import "time"

type (
	ActivityType         string
	ActivityStatus       string
	ActivityMessageLevel string
)

const (
	ActivityStatusQueued    ActivityStatus = "queued"
	ActivityStatusRunning   ActivityStatus = "running"
	ActivityStatusSuccess   ActivityStatus = "success"
	ActivityStatusFailed    ActivityStatus = "failed"
	ActivityStatusCancelled ActivityStatus = "cancelled"
)

const (
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
	ActivityTypeContainerDelete   ActivityType = "container_delete"
	ActivityTypeContainerKill     ActivityType = "container_kill"
	ActivityTypeContainerPause    ActivityType = "container_pause"
	ActivityTypeContainerUnpause  ActivityType = "container_unpause"
	ActivityTypeVulnerabilityScan ActivityType = "vulnerability_scan"
	ActivityTypeAutoUpdate        ActivityType = "auto_update"
	ActivityTypeSystemPrune       ActivityType = "system_prune"
	ActivityTypeResourceAction    ActivityType = "resource_action"
)

const (
	ActivityMessageLevelInfo    ActivityMessageLevel = "info"
	ActivityMessageLevelWarning ActivityMessageLevel = "warning"
	ActivityMessageLevelError   ActivityMessageLevel = "error"
	ActivityMessageLevelSuccess ActivityMessageLevel = "success"
)

type Activity struct {
	BaseModel

	EnvironmentID        string         `json:"environmentId" gorm:"column:environment_id;not null;index" sortable:"true"`
	BatchID              *string        `json:"batchId,omitempty" gorm:"column:batch_id;index"`
	Type                 ActivityType   `json:"type" gorm:"column:type;not null;index" sortable:"true"`
	Status               ActivityStatus `json:"status" gorm:"column:status;not null;index" sortable:"true"`
	ResourceType         *string        `json:"resourceType,omitempty" gorm:"column:resource_type;index" sortable:"true"`
	ResourceID           *string        `json:"resourceId,omitempty" gorm:"column:resource_id;index"`
	ResourceName         *string        `json:"resourceName,omitempty" gorm:"column:resource_name" sortable:"true"`
	Progress             *int           `json:"progress,omitempty" gorm:"column:progress"`
	Step                 string         `json:"step,omitempty" gorm:"column:step"`
	LatestMessage        string         `json:"latestMessage,omitempty" gorm:"column:latest_message"`
	StartedByUserID      *string        `json:"startedByUserId,omitempty" gorm:"column:started_by_user_id;index"`
	StartedByUsername    *string        `json:"startedByUsername,omitempty" gorm:"column:started_by_username"`
	StartedByDisplayName *string        `json:"startedByDisplayName,omitempty" gorm:"column:started_by_display_name"`
	StartedAt            time.Time      `json:"startedAt" gorm:"column:started_at;not null" sortable:"true"`
	EndedAt              *time.Time     `json:"endedAt,omitempty" gorm:"column:ended_at" sortable:"true"`
	DurationMs           *int64         `json:"durationMs,omitempty" gorm:"column:duration_ms" sortable:"true"`
	Error                *string        `json:"error,omitempty" gorm:"column:error"`
	Metadata             JSON           `json:"metadata,omitempty" gorm:"type:text"`
}

func (Activity) TableName() string {
	return "activities"
}

type ActivityMessage struct {
	BaseModel

	ActivityID string               `json:"activityId" gorm:"column:activity_id;not null;index"`
	Level      ActivityMessageLevel `json:"level" gorm:"column:level;not null"`
	Message    string               `json:"message" gorm:"column:message;not null"`
	Payload    JSON                 `json:"payload,omitempty" gorm:"type:text"`
	Activity   *Activity            `json:"-" gorm:"foreignKey:ActivityID;constraint:OnDelete:CASCADE"`
}

func (ActivityMessage) TableName() string {
	return "activity_messages"
}
