package jobschedule

import "time"

// Config represents the configured intervals (in minutes) for Arcane background jobs.
//
// All fields are in minutes.
// This makes conversion to time.Duration straightforward in the backend.
type Config struct {
	EnvironmentHealthInterval  string `json:"environmentHealthInterval"`
	EventCleanupInterval       string `json:"eventCleanupInterval"`
	AnalyticsHeartbeatInterval string `json:"analyticsHeartbeatInterval"`
}

// Update is used to update job schedule intervals (in minutes).
//
// Any nil field is ignored.
type Update struct {
	EnvironmentHealthInterval  *string `json:"environmentHealthInterval,omitempty"`
	EventCleanupInterval       *string `json:"eventCleanupInterval,omitempty"`
	AnalyticsHeartbeatInterval *string `json:"analyticsHeartbeatInterval,omitempty"`
}

// JobStatus represents the current status and metadata for a background job.
type JobStatus struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Category      string            `json:"category"`
	Schedule      string            `json:"schedule"`
	NextRun       *time.Time        `json:"nextRun,omitempty"`
	Enabled       bool              `json:"enabled"`
	ManagerOnly   bool              `json:"managerOnly"`
	IsContinuous  bool              `json:"isContinuous"`
	CanRunManually bool             `json:"canRunManually"`
	Prerequisites []JobPrerequisite `json:"prerequisites"`
	SettingsKey   string            `json:"settingsKey,omitempty"`
}

// JobPrerequisite represents a requirement that must be met for a job to run.
type JobPrerequisite struct {
	SettingKey  string `json:"settingKey"`
	Label       string `json:"label"`
	IsMet       bool   `json:"isMet"`
	SettingsURL string `json:"settingsUrl,omitempty"`
}

// JobListResponse contains all jobs and system mode information.
type JobListResponse struct {
	Jobs    []JobStatus `json:"jobs"`
	IsAgent bool        `json:"isAgent"`
}

// JobRunRequest is the request to manually run a job.
type JobRunRequest struct {
	JobID string `json:"jobId" path:"jobId" minLength:"1"`
}

// JobRunResponse is the response after manually triggering a job.
type JobRunResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
