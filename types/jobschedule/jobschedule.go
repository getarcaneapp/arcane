package jobschedule

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// Config contains cron schedules for Arcane background jobs.
type Config struct {
	EnvironmentHealthInterval      string `json:"environmentHealthInterval"`
	EventCleanupInterval           string `json:"eventCleanupInterval"`
	ExpiredSessionsCleanupInterval string `json:"expiredSessionsCleanupInterval"`
	AutoUpdateInterval             string `json:"autoUpdateInterval"`
	DockerClientRefreshInterval    string `json:"dockerClientRefreshInterval"`
	PollingInterval                string `json:"pollingInterval"`
	ScheduledPruneInterval         string `json:"scheduledPruneInterval"`
	VulnerabilityScanInterval      string `json:"vulnerabilityScanInterval"`
	ImageAutoPatchInterval         string `json:"imageAutoPatchInterval"`
	AutoHealInterval               string `json:"autoHealInterval"`
}

// Update changes job cron schedules.
//
// Any nil field is ignored.
type Update struct {
	EnvironmentHealthInterval      *string `json:"environmentHealthInterval,omitzero"`
	EventCleanupInterval           *string `json:"eventCleanupInterval,omitzero"`
	ExpiredSessionsCleanupInterval *string `json:"expiredSessionsCleanupInterval,omitzero"`
	AutoUpdateInterval             *string `json:"autoUpdateInterval,omitzero"`
	DockerClientRefreshInterval    *string `json:"dockerClientRefreshInterval,omitzero"`
	PollingInterval                *string `json:"pollingInterval,omitzero"`
	ScheduledPruneInterval         *string `json:"scheduledPruneInterval,omitzero"`
	VulnerabilityScanInterval      *string `json:"vulnerabilityScanInterval,omitzero"`
	ImageAutoPatchInterval         *string `json:"imageAutoPatchInterval,omitzero"`
	AutoHealInterval               *string `json:"autoHealInterval,omitzero"`
}

// JobStatus represents the current status and metadata for a background job.
type JobStatus struct {
	CurrentRun     *scheduler.Run          `json:"currentRun,omitempty"`
	LastRun        *scheduler.Run          `json:"lastRun,omitempty"`
	LastSuccess    *time.Time              `json:"lastSuccess,omitempty"`
	LastError      string                  `json:"lastError,omitempty"`
	WorkerHealth   *scheduler.WorkerHealth `json:"workerHealth,omitempty"`
	Children       []JobStatus             `json:"children,omitempty"`
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Description    string                  `json:"description"`
	Category       string                  `json:"category"`
	Schedule       string                  `json:"schedule"`
	NextRun        *time.Time              `json:"nextRun,omitempty"`
	Enabled        bool                    `json:"enabled"`
	ManagerOnly    bool                    `json:"managerOnly"`
	IsContinuous   bool                    `json:"isContinuous"`
	CanRunManually bool                    `json:"canRunManually"`
	Prerequisites  []JobPrerequisite       `json:"prerequisites"`
	SettingsKey    string                  `json:"settingsKey,omitempty"`
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
	DurableRuns bool        `json:"durableRuns"`
	ObservedAt  time.Time   `json:"observedAt"`
	Offline     bool        `json:"offline,omitempty"`
	Jobs        []JobStatus `json:"jobs"`
	IsAgent     bool        `json:"isAgent"`
}

// JobRunRequest is the request to manually run a job.
type JobRunRequest struct {
	JobID string `json:"jobId" path:"jobId" minLength:"1"`
}

// JobRunResponse is the response after manually triggering a job.
type JobRunResponse struct {
	RunID   string              `json:"runId,omitempty"`
	Status  scheduler.RunStatus `json:"status,omitempty"`
	Success bool                `json:"success"`
	Message string              `json:"message"`
}

// SubmitRunInput preserves identity across manager-to-agent delivery retries.
type SubmitRunInput struct {
	RunID string `json:"runId"`
}
