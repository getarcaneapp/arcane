package activity

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
)

type Activity struct {
	database.BaseModel

	EnvironmentID        string               `json:"environmentId" gorm:"column:environment_id;not null;index" sortable:"true"`
	BatchID              *string              `json:"batchId,omitempty" gorm:"column:batch_id;index"`
	Type                 activitytypes.Type   `json:"type" gorm:"column:type;not null;index" sortable:"true"`
	Status               activitytypes.Status `json:"status" gorm:"column:status;not null;index" sortable:"true"`
	ResourceType         *string              `json:"resourceType,omitempty" gorm:"column:resource_type;index" sortable:"true"`
	ResourceID           *string              `json:"resourceId,omitempty" gorm:"column:resource_id;index"`
	ResourceName         *string              `json:"resourceName,omitempty" gorm:"column:resource_name" sortable:"true"`
	Progress             *int                 `json:"progress,omitempty" gorm:"column:progress"`
	Step                 string               `json:"step,omitempty" gorm:"column:step"`
	LatestMessage        string               `json:"latestMessage,omitempty" gorm:"column:latest_message"`
	StartedByUserID      *string              `json:"startedByUserId,omitempty" gorm:"column:started_by_user_id;index"`
	StartedByUsername    *string              `json:"startedByUsername,omitempty" gorm:"column:started_by_username"`
	StartedByDisplayName *string              `json:"startedByDisplayName,omitempty" gorm:"column:started_by_display_name"`
	StartedAt            time.Time            `json:"startedAt" gorm:"column:started_at;not null" sortable:"true"`
	EndedAt              *time.Time           `json:"endedAt,omitempty" gorm:"column:ended_at" sortable:"true"`
	DurationMs           *int64               `json:"durationMs,omitempty" gorm:"column:duration_ms" sortable:"true"`
	Error                *string              `json:"error,omitempty" gorm:"column:error"`
	Metadata             database.JSON        `json:"metadata,omitempty" gorm:"type:text"`
}

func (Activity) TableName() string {
	return "activities"
}

type ActivityMessage struct {
	database.BaseModel

	ActivityID string                     `json:"activityId" gorm:"column:activity_id;not null;index"`
	Level      activitytypes.MessageLevel `json:"level" gorm:"column:level;not null"`
	Message    string                     `json:"message" gorm:"column:message;not null"`
	Payload    database.JSON              `json:"payload,omitempty" gorm:"type:text"`
	Activity   *Activity                  `json:"-" gorm:"foreignKey:ActivityID;constraint:OnDelete:CASCADE"`
}

func (ActivityMessage) TableName() string {
	return "activity_messages"
}
