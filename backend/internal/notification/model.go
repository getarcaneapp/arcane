package notification

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/notifications"
)

type (
	NotificationSettings struct {
		CreatedAt time.Time                          `json:"createdAt"`
		UpdatedAt time.Time                          `json:"updatedAt"`
		Config    database.JSON                      `json:"config" gorm:"type:jsonb"`
		Provider  notifications.NotificationProvider `json:"provider" gorm:"not null;index;type:varchar(50)"`
		ID        uint                               `json:"id" gorm:"primaryKey"`
		Enabled   bool                               `json:"enabled" gorm:"default:false"`
	}
)

func (NotificationSettings) TableName() string {
	return "notification_settings"
}
