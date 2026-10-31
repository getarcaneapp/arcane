package activity

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
)

type Activity struct {
	database.BaseModel

	StartedAt            time.Time     `json:"startedAt" gorm:"column:started_at;not null" sortable:"true"`
	BatchID              *string       `json:"batchId,omitempty" gorm:"column:batch_id;index"`
	ResourceType         *string       `json:"resourceType,omitempty" gorm:"column:resource_type;index" sortable:"true"`
	ResourceID           *string       `json:"resourceId,omitempty" gorm:"column:resource_id;index"`
	ResourceName         *string       `json:"resourceName,omitempty" gorm:"column:resource_name" sortable:"true"`
	Progress             *int          `json:"progress,omitempty" gorm:"column:progress"`
	StartedByUserID      *string       `json:"startedByUserId,omitempty" gorm:"column:started_by_user_id;index"`
	StartedByUsername    *string       `json:"startedByUsername,omitempty" gorm:"column:started_by_username"`
	StartedByDisplayName *string       `json:"startedByDisplayName,omitempty" gorm:"column:started_by_display_name"`
	EndedAt              *time.Time    `json:"endedAt,omitempty" gorm:"column:ended_at" sortable:"true"`
	DurationMs           *int64        `json:"durationMs,omitempty" gorm:"column:duration_ms" sortable:"true"`
	Error                *string       `json:"error,omitempty" gorm:"column:error"`
	Metadata             database.JSON `json:"metadata,omitempty" gorm:"type:text"`

	EnvironmentID string               `json:"environmentId" gorm:"column:environment_id;not null;index" sortable:"true"`
	Type          types.ActivityType   `json:"type" gorm:"column:type;not null;index" sortable:"true"`
	Status        types.ActivityStatus `json:"status" gorm:"column:status;not null;index" sortable:"true"`
	Step          string               `json:"step,omitempty" gorm:"column:step"`
	LatestMessage string               `json:"latestMessage,omitempty" gorm:"column:latest_message"`
}

func (Activity) TableName() string {
	return "activities"
}

type ActivityMessage struct {
	database.BaseModel

	Payload  database.JSON `json:"payload,omitempty" gorm:"type:text"`
	Activity *Activity     `json:"-" gorm:"foreignKey:ActivityID;constraint:OnDelete:CASCADE"`

	ActivityID string                     `json:"activityId" gorm:"column:activity_id;not null;index"`
	Level      types.ActivityMessageLevel `json:"level" gorm:"column:level;not null"`
	Message    string                     `json:"message" gorm:"column:message;not null"`
}

func (ActivityMessage) TableName() string {
	return "activity_messages"
}
