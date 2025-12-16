package models

import (
	"time"
)

type ImageUpdateRecord struct {
	ID             string    `json:"id" gorm:"primaryKey;type:text"`
	Repository     string    `json:"repository"`
	Tag            string    `json:"tag"`
	HasUpdate      bool      `json:"hasUpdate" gorm:"column:has_update"`
	UpdateType     string    `json:"updateType" gorm:"column:update_type"`
	CurrentVersion string    `json:"currentVersion" gorm:"column:current_version"`
	LatestVersion  *string   `json:"latestVersion,omitempty" gorm:"column:latest_version"`
	CurrentDigest  *string   `json:"currentDigest,omitempty" gorm:"column:current_digest"`
	LatestDigest   *string   `json:"latestDigest,omitempty" gorm:"column:latest_digest"`
	CheckTime      time.Time `json:"checkTime" gorm:"column:check_time"`
	ResponseTimeMs int       `json:"responseTimeMs" gorm:"column:response_time_ms"`
	LastError      *string   `json:"lastError,omitempty" gorm:"column:last_error"`

	AuthMethod     *string `json:"authMethod,omitempty" gorm:"column:auth_method"`
	AuthUsername   *string `json:"authUsername,omitempty" gorm:"column:auth_username"`
	AuthRegistry   *string `json:"authRegistry,omitempty" gorm:"column:auth_registry"`
	UsedCredential bool    `json:"usedCredential,omitempty" gorm:"column:used_credential"`

	NotificationSent bool `json:"notificationSent" gorm:"column:notification_sent;default:false"`

	BaseModel
}

func (i *ImageUpdateRecord) TableName() string {
	return "image_updates"
}

type ImageUpdate struct {
	HasUpdate      bool   `json:"hasUpdate"`
	UpdateType     string `json:"updateType"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	CheckTime      string `json:"checkTime"`
}

const (
	UpdateTypeDigest = "digest"
	UpdateTypeTag    = "tag"
)

func (i *ImageUpdateRecord) NeedsUpdate() bool {
	return i.HasUpdate
}

func (i *ImageUpdateRecord) IsDigestUpdate() bool {
	return i.UpdateType == UpdateTypeDigest
}

func (i *ImageUpdateRecord) IsTagUpdate() bool {
	return i.UpdateType == UpdateTypeTag
}
