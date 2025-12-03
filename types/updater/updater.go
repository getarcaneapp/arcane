package updater

type Options struct {
	// Type filters updates by resource type ("image" | "container" | "project")
	Type string `json:"type,omitempty"`

	// ResourceIds limits updates to specific resources
	ResourceIds []string `json:"resourceIds,omitempty"`

	// ForceUpdate forces updates even if up to date
	ForceUpdate bool `json:"forceUpdate,omitempty"`

	// DryRun performs a dry run without applying updates
	DryRun bool `json:"dryRun,omitempty"`
}

// ResourceResult represents the result of an update operation on a single resource.
type ResourceResult struct {
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

	// Status is the current status ("checked" | "updated" | "skipped" | "failed" | "up_to_date" | "update_available").
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
	Details map[string]interface{} `json:"details,omitempty"`
}

// Result represents the complete result of an update operation.
type Result struct {
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
	Items []ResourceResult `json:"items"`
}
