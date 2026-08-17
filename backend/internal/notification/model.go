package notification

import (
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
)

type NotificationSettings struct {
	ID        uint                               `json:"id" gorm:"primaryKey"`
	Provider  notifications.NotificationProvider `json:"provider" gorm:"not null;index;type:varchar(50)"`
	Enabled   bool                               `json:"enabled" gorm:"default:false"`
	Config    database.JSON                      `json:"config" gorm:"type:jsonb"`
	CreatedAt time.Time                          `json:"createdAt"`
	UpdatedAt time.Time                          `json:"updatedAt"`
}

func (NotificationSettings) TableName() string {
	return "notification_settings"
}
