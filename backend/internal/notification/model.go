package notification

import (
	"time"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/notifications"
)

type NotificationSettings struct {
	CreatedAt time.Time                          `json:"createdAt"`
	UpdatedAt time.Time                          `json:"updatedAt"`
	Config    database.JSON                      `json:"config" gorm:"type:jsonb"`
	Provider  notifications.NotificationProvider `json:"provider" gorm:"not null;index;type:varchar(50)"`
	ID        uint                               `json:"id" gorm:"primaryKey"`
	Enabled   bool                               `json:"enabled" gorm:"default:false"`
}

func (NotificationSettings) TableName() string {
	return "notification_settings"
}
