package updater

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"time"
)

type AutoUpdateStatus string

const (
	AutoUpdateStatusPending   AutoUpdateStatus = "pending"
	AutoUpdateStatusChecking  AutoUpdateStatus = "checking"
	AutoUpdateStatusUpdating  AutoUpdateStatus = "updating"
	AutoUpdateStatusCompleted AutoUpdateStatus = "completed"
	AutoUpdateStatusFailed    AutoUpdateStatus = "failed"
	AutoUpdateStatusSkipped   AutoUpdateStatus = "skipped"
	AutoUpdateStatusRestarted AutoUpdateStatus = "restarted"
)

type AutoUpdateRecord struct {
	database.BaseModel

	ResourceID       string           `json:"resourceId"`
	ResourceType     string           `json:"resourceType"`
	ResourceName     string           `json:"resourceName"`
	Status           AutoUpdateStatus `json:"status"`
	StartTime        time.Time        `json:"startTime"`
	EndTime          *time.Time       `json:"endTime,omitempty"`
	UpdateAvailable  bool             `json:"updateAvailable"`
	UpdateApplied    bool             `json:"updateApplied"`
	OldImageVersions database.JSON    `json:"oldImageVersions,omitempty" gorm:"type:text"`
	NewImageVersions database.JSON    `json:"newImageVersions,omitempty" gorm:"type:text"`
	Error            *string          `json:"error,omitempty"`
	Details          database.JSON    `json:"details,omitempty" gorm:"type:text"`
}

func (AutoUpdateRecord) TableName() string {
	return "auto_update_records"
}
