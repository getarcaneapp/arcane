package apns

import (
	"time"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
)

type (
	Device struct {
		database.BaseModel

		Events     map[string]bool `json:"events" gorm:"column:events;serializer:json"`
		LastSeenAt *time.Time      `json:"lastSeenAt,omitempty" gorm:"column:last_seen_at"`

		UserID         string               `json:"userId" gorm:"column:user_id;not null;index"`
		RecipientID    string               `json:"recipientId" gorm:"column:recipient_id;not null;uniqueIndex"`
		Label          string               `json:"label" gorm:"column:label;not null;default:''"`
		EnvironmentIDs database.StringSlice `json:"environmentIds" gorm:"column:environment_ids;type:text"`
	}

	OutboxEntry struct {
		database.BaseModel

		NextAttemptAt time.Time `gorm:"column:next_attempt_at;not null;index"`
		LastError     *string   `gorm:"column:last_error"`

		EventID  string `gorm:"column:event_id;not null"`
		Envelope string `gorm:"column:envelope;not null"`
		Attempts int    `gorm:"column:attempts;not null;default:0"`
	}
)

func (Device) TableName() string {
	return "apns_devices"
}

func (OutboxEntry) TableName() string {
	return "apns_outbox"
}
