package models

import (
	"time"

	snippettypes "github.com/getarcaneapp/arcane/types/v2/snippet"
)

// Snippet is a user-authored host-shell command with typed parameters,
// runnable manually or on a cron schedule. Execution is env-scoped: a
// snippet is defined on, stored by, and runs on the environment that owns
// it (see HostShellService).
type Snippet struct {
	BaseModel

	EnvironmentID      string                      `json:"environmentId" sortable:"true"`
	Name               string                      `json:"name" sortable:"true" search:"snippet,command,script"`
	Description        *string                     `json:"description,omitempty"`
	Script             string                      `json:"script"`
	Target             string                      `json:"target" gorm:"column:target;default:host"`
	ContainerID        *string                     `json:"containerId,omitempty" gorm:"column:container_id"`
	Parameters         []snippettypes.ParameterDef `json:"parameters,omitempty" gorm:"serializer:json"`
	WorkingDir         *string                     `json:"workingDir,omitempty" gorm:"column:working_dir"`
	TimeoutSec         int                         `json:"timeoutSec" gorm:"column:timeout_sec;default:60"`
	Schedule           *string                     `json:"schedule,omitempty"`
	ScheduleEnabled    bool                        `json:"scheduleEnabled" gorm:"column:schedule_enabled"`
	ScheduleParameters map[string]string           `json:"scheduleParameters,omitempty" gorm:"column:schedule_parameters;serializer:json"`
	LastRunAt          *time.Time                  `json:"lastRunAt,omitempty" gorm:"column:last_run_at" sortable:"true"`
	LastRunStatus      *string                     `json:"lastRunStatus,omitempty" gorm:"column:last_run_status" sortable:"true"`
	CreatedByUserID    *string                     `json:"createdByUserId,omitempty" gorm:"column:created_by_user_id"`
}

func (Snippet) TableName() string {
	return "snippets"
}

// SnippetRun is one execution of a Snippet, manual or scheduled. Kept in its
// own table rather than Event.Metadata: events are purged at 36 hours and
// Event.Metadata is not sized for a capped output blob.
type SnippetRun struct {
	BaseModel

	SnippetID         string            `json:"snippetId" gorm:"column:snippet_id" sortable:"true"`
	EnvironmentID     string            `json:"environmentId" sortable:"true"`
	TriggerSource     string            `json:"triggerSource" gorm:"column:trigger_source" search:"manual,schedule,trigger"`
	Status            string            `json:"status" sortable:"true" search:"success,failed,timeout,status"`
	ExitCode          *int64            `json:"exitCode,omitempty" gorm:"column:exit_code"`
	Parameters        map[string]string `json:"parameters,omitempty" gorm:"serializer:json"`
	Output            *string           `json:"output,omitempty"`
	Error             *string           `json:"error,omitempty"`
	StartedAt         time.Time         `json:"startedAt" gorm:"column:started_at" sortable:"true"`
	DurationMs        int64             `json:"durationMs" gorm:"column:duration_ms"`
	StartedByUserID   *string           `json:"startedByUserId,omitempty" gorm:"column:started_by_user_id"`
	StartedByUsername *string           `json:"startedByUsername,omitempty" gorm:"column:started_by_username"`
}

func (SnippetRun) TableName() string {
	return "snippet_runs"
}
