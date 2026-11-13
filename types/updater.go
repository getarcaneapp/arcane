package types

import (
	"time"
)

type (
	AutoUpdateRecord struct {
		CreatedAt        time.Time      `json:"createdAt" sortable:"true"`
		StartTime        time.Time      `json:"startTime"`
		UpdatedAt        *time.Time     `json:"updatedAt,omitempty"`
		EndTime          *time.Time     `json:"endTime,omitempty"`
		OldImageVersions map[string]any `json:"oldImageVersions,omitempty"`
		NewImageVersions map[string]any `json:"newImageVersions,omitempty"`
		Error            *string        `json:"error,omitempty"`
		Details          map[string]any `json:"details,omitempty"`
		EnvironmentTag

		ID              string `json:"id"`
		ResourceID      string `json:"resourceId"`
		ResourceType    string `json:"resourceType"`
		ResourceName    string `json:"resourceName"`
		Status          string `json:"status"`
		UpdateAvailable bool   `json:"updateAvailable"`
		UpdateApplied   bool   `json:"updateApplied"`
	}

	UpdaterOptions struct {
		Type        string   `json:"type,omitempty"`
		ResourceIds []string `json:"resourceIds,omitempty"`
		ForceUpdate bool     `json:"forceUpdate,omitempty"`
		DryRun      bool     `json:"dryRun,omitempty"`

		IgnoreSettingsExclusions bool `json:"-"`
	}

	UpdaterResourceResult struct {
		OldImages       map[string]string `json:"oldImages,omitempty"`
		NewImages       map[string]string `json:"newImages,omitempty"`
		Details         map[string]any    `json:"details,omitempty"`
		ResourceID      string            `json:"resourceId"`
		ResourceName    string            `json:"resourceName,omitempty"`
		ResourceType    string            `json:"resourceType"`
		Status          string            `json:"status"`
		Error           string            `json:"error,omitempty"`
		UpdateAvailable bool              `json:"updateAvailable,omitempty"`
		UpdateApplied   bool              `json:"updateApplied,omitempty"`
	}

	UpdaterResult struct {
		ActivityID *string                 `json:"activityId,omitempty"`
		StartTime  string                  `json:"startTime,omitempty"`
		EndTime    string                  `json:"endTime,omitempty"`
		Duration   string                  `json:"duration"`
		Items      []UpdaterResourceResult `json:"items"`
		Checked    int                     `json:"checked"`
		Updated    int                     `json:"updated"`
		Restarted  int                     `json:"restarted,omitempty"`
		Skipped    int                     `json:"skipped"`
		Failed     int                     `json:"failed"`
		Success    bool                    `json:"success,omitempty"`
	}

	UpdaterStatus struct {
		ContainerIds       []string `json:"containerIds"`
		ProjectIds         []string `json:"projectIds"`
		UpdatingContainers int      `json:"updatingContainers"`
		UpdatingProjects   int      `json:"updatingProjects"`
	}

	UpdaterCheckProjectInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
	}

	FrozenUpdateTarget struct {
		ContainerID          string
		ContainerName        string
		ComposeProject       string
		ComposeService       string
		ComposeNumber        string
		BaselineImageID      string
		BaselineStartedAt    string
		DesiredImageRef      string
		DesiredDigest        string
		BaselineRestartCount int
		BaselineRunning      bool
	}
)

const (
	UpdaterStatusChecked         = "checked"
	UpdaterStatusUpdated         = "updated"
	UpdaterStatusRestarted       = "restarted"
	UpdaterStatusSkipped         = "skipped"
	UpdaterStatusFailed          = "failed"
	UpdaterStatusBlocked         = "blocked"
	UpdaterStatusUpToDate        = "up_to_date"
	UpdaterStatusUpdateAvailable = "update_available"
)
