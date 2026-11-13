package updater

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	AutoUpdateStatus string

	AutoUpdateRecord struct {
		database.BaseModel

		StartTime        time.Time     `json:"startTime"`
		EndTime          *time.Time    `json:"endTime,omitempty"`
		OldImageVersions database.JSON `json:"oldImageVersions,omitempty" gorm:"type:text"`
		NewImageVersions database.JSON `json:"newImageVersions,omitempty" gorm:"type:text"`
		Error            *string       `json:"error,omitempty"`
		Details          database.JSON `json:"details,omitempty" gorm:"type:text"`

		ResourceID      string           `json:"resourceId"`
		ResourceType    string           `json:"resourceType"`
		ResourceName    string           `json:"resourceName"`
		Status          AutoUpdateStatus `json:"status"`
		UpdateAvailable bool             `json:"updateAvailable"`
		UpdateApplied   bool             `json:"updateApplied"`
	}

	vulnerabilityGate struct {
		limits  types.VulnerabilitySeveritySummary
		policy  string
		enabled bool
	}
)

const (
	AutoUpdateStatusPending   AutoUpdateStatus = "pending"
	AutoUpdateStatusChecking  AutoUpdateStatus = "checking"
	AutoUpdateStatusUpdating  AutoUpdateStatus = "updating"
	AutoUpdateStatusCompleted AutoUpdateStatus = "completed"
	AutoUpdateStatusFailed    AutoUpdateStatus = "failed"
	AutoUpdateStatusBlocked   AutoUpdateStatus = "blocked"
	AutoUpdateStatusSkipped   AutoUpdateStatus = "skipped"
	AutoUpdateStatusRestarted AutoUpdateStatus = "restarted"

	vulnerabilityGateBlockedPrefix    = "update blocked by vulnerability gate: "
	vulnerabilityGateScanFailedPrefix = "vulnerability gate scan failed: "
)

func (AutoUpdateRecord) TableName() string {
	return "auto_update_records"
}
