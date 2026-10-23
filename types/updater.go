package types

import (
	"time"
)

// AutoUpdateRecord reports a persisted resource auto-update operation.
type AutoUpdateRecord struct {
	EnvironmentTag

	ID               string         `json:"id"`
	CreatedAt        time.Time      `json:"createdAt" sortable:"true"`
	UpdatedAt        *time.Time     `json:"updatedAt,omitempty"`
	ResourceID       string         `json:"resourceId"`
	ResourceType     string         `json:"resourceType"`
	ResourceName     string         `json:"resourceName"`
	Status           string         `json:"status"`
	StartTime        time.Time      `json:"startTime"`
	EndTime          *time.Time     `json:"endTime,omitempty"`
	UpdateAvailable  bool           `json:"updateAvailable"`
	UpdateApplied    bool           `json:"updateApplied"`
	OldImageVersions map[string]any `json:"oldImageVersions,omitempty"`
	NewImageVersions map[string]any `json:"newImageVersions,omitempty"`
	Error            *string        `json:"error,omitempty"`
	Details          map[string]any `json:"details,omitempty"`
}

const (
	// UpdaterStatusChecked indicates a resource was checked.
	UpdaterStatusChecked = "checked"
	// UpdaterStatusUpdated indicates a resource was updated.
	UpdaterStatusUpdated = "updated"
	// UpdaterStatusRestarted indicates a resource was restarted because a dependency changed.
	UpdaterStatusRestarted = "restarted"
	// UpdaterStatusSkipped indicates a resource was skipped.
	UpdaterStatusSkipped = "skipped"
	// UpdaterStatusFailed indicates a resource failed to update.
	UpdaterStatusFailed = "failed"
	// UpdaterStatusUpToDate indicates a resource is already up to date.
	UpdaterStatusUpToDate = "up_to_date"
	// UpdaterStatusUpdateAvailable indicates an update is available.
	UpdaterStatusUpdateAvailable = "update_available"
)

type UpdaterOptions struct {
	// Type filters updates by resource type ("image" | "container" | "project")
	Type string `json:"type,omitempty"`

	// ResourceIds limits updates to specific resources
	ResourceIds []string `json:"resourceIds,omitempty"`

	// ForceUpdate forces updates even if up to date
	ForceUpdate bool `json:"forceUpdate,omitempty"`

	// DryRun performs a dry run without applying updates
	DryRun bool `json:"dryRun,omitempty"`

	IgnoreSettingsExclusions bool `json:"-"`
}

// UpdaterResourceResult represents the result of an update operation on a single resource.
type UpdaterResourceResult struct {
	// ResourceID is the unique identifier of the resource.
	//
	// Required: true
	ResourceID string `json:"resourceId"`

	// ResourceName is the name of the resource.
	//
	// Required: false
	ResourceName string `json:"resourceName,omitempty"`

	// ResourceType is the type of the resource ("image" | "container" | "project").
	//
	// Required: true
	ResourceType string `json:"resourceType"`

	// Status is the current status ("checked" | "updated" | "restarted" | "skipped" | "failed" | "up_to_date" | "update_available").
	//
	// Required: true
	Status string `json:"status"`

	// UpdateAvailable indicates if an update is available.
	//
	// Required: false
	UpdateAvailable bool `json:"updateAvailable,omitempty"`

	// UpdateApplied indicates if an update was applied.
	//
	// Required: false
	UpdateApplied bool `json:"updateApplied,omitempty"`

	// OldImages is a map of old image references.
	//
	// Required: false
	OldImages map[string]string `json:"oldImages,omitempty"`

	// NewImages is a map of new image references.
	//
	// Required: false
	NewImages map[string]string `json:"newImages,omitempty"`

	// Error contains any error message encountered during the update.
	//
	// Required: false
	Error string `json:"error,omitempty"`

	// Details contains additional details about the update operation.
	//
	// Required: false
	Details map[string]any `json:"details,omitempty"`
}

// UpdaterResult represents the complete result of an update operation.
type UpdaterResult struct {
	// Success indicates if the overall update operation was successful.
	//
	// Required: false
	Success bool `json:"success,omitempty"`

	// Checked is the number of resources checked.
	//
	// Required: true
	Checked int `json:"checked"`

	// Updated is the number of resources updated.
	//
	// Required: true
	Updated int `json:"updated"`

	// Restarted is the number of resources restarted because a dependency changed.
	//
	// Required: false
	Restarted int `json:"restarted,omitempty"`

	// Skipped is the number of resources skipped.
	//
	// Required: true
	Skipped int `json:"skipped"`

	// Failed is the number of resources that failed to update.
	//
	// Required: true
	Failed int `json:"failed"`

	// StartTime is the time when the update operation started.
	//
	// Required: false
	StartTime string `json:"startTime,omitempty"`

	// EndTime is the time when the update operation ended.
	//
	// Required: false
	EndTime string `json:"endTime,omitempty"`

	// Duration is the total duration of the update operation.
	//
	// Required: true
	Duration string `json:"duration"`

	// Items is a list of individual resource results.
	//
	// Required: true
	Items []UpdaterResourceResult `json:"items"`

	// ActivityID is the background activity that tracked this update operation.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`
}

// UpdaterStatus represents the current status of the updater.
type UpdaterStatus struct {
	// UpdatingContainers is the count of containers currently being updated.
	//
	// Required: true
	UpdatingContainers int `json:"updatingContainers"`

	// UpdatingProjects is the count of projects currently being updated.
	//
	// Required: true
	UpdatingProjects int `json:"updatingProjects"`

	// ContainerIds is a list of container IDs currently being updated.
	//
	// Required: true
	ContainerIds []string `json:"containerIds"`

	// ProjectIds is a list of project IDs currently being updated.
	//
	// Required: true
	ProjectIds []string `json:"projectIds"`
}

// UpdaterCheckProjectInput identifies the project whose service policies should be checked.
type UpdaterCheckProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

// FrozenUpdateTarget identifies the selected container and immutable image intent.
type FrozenUpdateTarget struct {
	ContainerID          string
	ContainerName        string
	ComposeProject       string
	ComposeService       string
	ComposeNumber        string
	BaselineImageID      string
	BaselineStartedAt    string
	BaselineRestartCount int
	BaselineRunning      bool
	DesiredImageRef      string
	DesiredDigest        string
}
