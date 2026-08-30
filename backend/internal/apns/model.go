package apns

import (
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

type Device struct {
	database.BaseModel

	UserID         string               `json:"userId" gorm:"column:user_id;not null;index"`
	RecipientID    string               `json:"recipientId" gorm:"column:recipient_id;not null;uniqueIndex"`
	Label          string               `json:"label" gorm:"column:label;not null;default:''"`
	Events         map[string]bool      `json:"events" gorm:"column:events;serializer:json"`
	EnvironmentIDs database.StringSlice `json:"environmentIds" gorm:"column:environment_ids;type:text"`
	LastSeenAt     *time.Time           `json:"lastSeenAt,omitempty" gorm:"column:last_seen_at"`
}

func (Device) TableName() string {
	return "apns_devices"
}

type OutboxEntry struct {
	database.BaseModel

	EventID       string    `gorm:"column:event_id;not null"`
	Envelope      string    `gorm:"column:envelope;not null"`
	Attempts      int       `gorm:"column:attempts;not null;default:0"`
	NextAttemptAt time.Time `gorm:"column:next_attempt_at;not null;index"`
	LastError     *string   `gorm:"column:last_error"`
}

func (OutboxEntry) TableName() string {
	return "apns_outbox"
}
