package models

import (
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
)

type AutoUpdateRecord struct {
	ResourceID       string           `json:"resourceId"`
	ResourceType     string           `json:"resourceType"`
	ResourceName     string           `json:"resourceName"`
	Status           AutoUpdateStatus `json:"status"`
	StartTime        time.Time        `json:"startTime"`
	EndTime          *time.Time       `json:"endTime,omitempty"`
	UpdateAvailable  bool             `json:"updateAvailable"`
	UpdateApplied    bool             `json:"updateApplied"`
	OldImageVersions JSON             `json:"oldImageVersions,omitempty" gorm:"type:text"`
	NewImageVersions JSON             `json:"newImageVersions,omitempty" gorm:"type:text"`
	Error            *string          `json:"error,omitempty"`
	Details          JSON             `json:"details,omitempty" gorm:"type:text"`
	BaseModel
}

func (AutoUpdateRecord) TableName() string {
	return "auto_update_records"
}
