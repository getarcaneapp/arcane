package models

import (
	"time"
)

type NotificationProvider string

const (
	NotificationProviderDiscord NotificationProvider = "discord"
	NotificationProviderEmail   NotificationProvider = "email"
)

type NotificationEventType string

const (
	NotificationEventImageUpdate     NotificationEventType = "image_update"
	NotificationEventContainerUpdate NotificationEventType = "container_update"
)

type EmailTLSMode string

const (
	EmailTLSModeNone     EmailTLSMode = "none"
	EmailTLSModeStartTLS EmailTLSMode = "starttls"
	EmailTLSModeSSL      EmailTLSMode = "ssl"
)

type NotificationSettings struct {
	ID        uint                 `json:"id" gorm:"primaryKey"`
	Name      string               `json:"name" gorm:"type:varchar(255)"`
	Provider  NotificationProvider `json:"provider" gorm:"not null;index;type:varchar(50)"`
	Enabled   bool                 `json:"enabled" gorm:"default:false"`
	Config    JSON                 `json:"config" gorm:"type:jsonb"`
	CreatedAt time.Time            `json:"createdAt"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

func (NotificationSettings) TableName() string {
	return "notification_settings"
}

type NotificationLog struct {
	ID        uint                 `json:"id" gorm:"primaryKey"`
	Provider  NotificationProvider `json:"provider" gorm:"not null;index;type:varchar(50)"`
	ImageRef  string               `json:"imageRef" gorm:"not null"`
	Status    string               `json:"status" gorm:"not null"`
	Error     *string              `json:"error,omitempty"`
	Metadata  JSON                 `json:"metadata" gorm:"type:jsonb"`
	SentAt    time.Time            `json:"sentAt" gorm:"not null;index"`
	CreatedAt time.Time            `json:"createdAt"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

func (NotificationLog) TableName() string {
	return "notification_logs"
}

type EmailConfig struct {
	SMTPHost     string                         `json:"smtpHost"`
	SMTPPort     int                            `json:"smtpPort"`
	SMTPUsername string                         `json:"smtpUsername"`
	SMTPPassword string                         `json:"smtpPassword"`
	FromAddress  string                         `json:"fromAddress"`
	ToAddresses  []string                       `json:"toAddresses"`
	TLSMode      EmailTLSMode                   `json:"tlsMode"`
	Events       map[NotificationEventType]bool `json:"events,omitempty"`
}

type AppriseSettings struct {
	ID                 uint      `json:"id" gorm:"primaryKey"`
	APIURL             string    `json:"apiUrl" gorm:"not null"`
	Enabled            bool      `json:"enabled" gorm:"default:false"`
	ImageUpdateTag     string    `json:"imageUpdateTag" gorm:"type:varchar(255)"`
	ContainerUpdateTag string    `json:"containerUpdateTag" gorm:"type:varchar(255)"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func (AppriseSettings) TableName() string {
	return "apprise_settings"
}
