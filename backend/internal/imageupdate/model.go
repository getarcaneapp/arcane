package imageupdate

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/samber/mo"

	"time"
)

type ImageUpdateRecord struct {
	database.BaseModel

	ProjectID   string `json:"projectId,omitempty" gorm:"column:project_id;not null;default:'';index"`
	ServiceName string `json:"serviceName,omitempty" gorm:"column:service_name;not null;default:''"`
	PolicyKey   string `json:"-" gorm:"column:policy_key;not null;default:''"`

	ContainerID string `json:"containerId,omitempty" gorm:"column:container_id;not null;default:'';index"`
	ImageID     string `json:"imageId,omitempty" gorm:"column:image_id;not null;default:'';index"`

	CheckTime        time.Time `json:"checkTime" gorm:"column:check_time"`
	LatestVersion    *string   `json:"latestVersion,omitempty" gorm:"column:latest_version"`
	CurrentDigest    *string   `json:"currentDigest,omitempty" gorm:"column:current_digest"`
	LatestDigest     *string   `json:"latestDigest,omitempty" gorm:"column:latest_digest"`
	LastError        *string   `json:"lastError,omitempty" gorm:"column:last_error"`
	AuthMethod       *string   `json:"authMethod,omitempty" gorm:"column:auth_method"`
	AuthUsername     *string   `json:"authUsername,omitempty" gorm:"column:auth_username"`
	AuthRegistry     *string   `json:"authRegistry,omitempty" gorm:"column:auth_registry"`
	ID               string    `json:"id" gorm:"primaryKey;type:text"`
	Repository       string    `json:"repository"`
	Tag              string    `json:"tag"`
	UpdateType       string    `json:"updateType" gorm:"column:update_type"`
	CurrentVersion   string    `json:"currentVersion" gorm:"column:current_version"`
	ResponseTimeMs   int       `json:"responseTimeMs" gorm:"column:response_time_ms"`
	HasUpdate        bool      `json:"hasUpdate" gorm:"column:has_update"`
	UsedCredential   bool      `json:"usedCredential,omitempty" gorm:"column:used_credential"`
	NotificationSent bool      `json:"notificationSent" gorm:"column:notification_sent;default:false"`
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
	UpdateTypeDigest    = "digest"
	UpdateTypeTag       = "tag"
	UpdateTypeLocal     = "local"
	UpdateTypeNotPulled = "not_pulled"
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

// UpdateInfo returns the public status of this check.
func (i *ImageUpdateRecord) UpdateInfo() *imagetypes.UpdateInfo {
	return &imagetypes.UpdateInfo{
		HasUpdate:      i.HasUpdate,
		UpdateType:     i.UpdateType,
		CurrentVersion: i.CurrentVersion,
		LatestVersion:  mo.PointerToOption(i.LatestVersion).OrEmpty(),
		CurrentDigest:  mo.PointerToOption(i.CurrentDigest).OrEmpty(),
		LatestDigest:   mo.PointerToOption(i.LatestDigest).OrEmpty(),
		CheckTime:      i.CheckTime,
		ResponseTimeMs: i.ResponseTimeMs,
		Error:          mo.PointerToOption(i.LastError).OrEmpty(),
		AuthMethod:     mo.PointerToOption(i.AuthMethod).OrEmpty(),
		AuthUsername:   mo.PointerToOption(i.AuthUsername).OrEmpty(),
		AuthRegistry:   mo.PointerToOption(i.AuthRegistry).OrEmpty(),
		UsedCredential: i.UsedCredential,
	}
}
