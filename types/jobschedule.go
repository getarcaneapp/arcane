package types

import (
	"time"
)

// JobScheduleConfig contains cron schedules for Arcane background jobs.
type JobScheduleConfig struct {
	EnvironmentHealthInterval      string `json:"environmentHealthInterval" required:"false"`
	EventCleanupInterval           string `json:"eventCleanupInterval" required:"false"`
	ExpiredSessionsCleanupInterval string `json:"expiredSessionsCleanupInterval" required:"false"`
	AutoUpdateInterval             string `json:"autoUpdateInterval" required:"false"`
	DockerClientRefreshInterval    string `json:"dockerClientRefreshInterval" required:"false"`
	PollingInterval                string `json:"pollingInterval" required:"false"`
	ScheduledPruneInterval         string `json:"scheduledPruneInterval" required:"false"`
	VulnerabilityScanInterval      string `json:"vulnerabilityScanInterval" required:"false"`
	ImageAutoPatchInterval         string `json:"imageAutoPatchInterval" required:"false"`
	AutoHealInterval               string `json:"autoHealInterval" required:"false"`
}

// JobStatus represents the current status and metadata for a background job.
type JobStatus struct {
	CurrentRun     *SchedulerRun          `json:"currentRun,omitempty"`
	LastRun        *SchedulerRun          `json:"lastRun,omitempty"`
	LastSuccess    *time.Time             `json:"lastSuccess,omitempty"`
	LastError      string                 `json:"lastError,omitempty"`
	WorkerHealth   *SchedulerWorkerHealth `json:"workerHealth,omitempty"`
	Children       []JobStatus            `json:"children,omitempty"`
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Category       string                 `json:"category"`
	Schedule       string                 `json:"schedule"`
	NextRun        *time.Time             `json:"nextRun,omitempty"`
	Enabled        bool                   `json:"enabled"`
	ManagerOnly    bool                   `json:"managerOnly"`
	IsContinuous   bool                   `json:"isContinuous"`
	CanRunManually bool                   `json:"canRunManually"`
	Prerequisites  []JobPrerequisite      `json:"prerequisites"`
	SettingsKey    string                 `json:"settingsKey,omitempty"`
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

// JobRunResponse is the response after manually triggering a job.
type JobRunResponse struct {
	RunID   string    `json:"runId,omitempty"`
	Status  RunStatus `json:"status,omitempty"`
	Success bool      `json:"success"`
	Message string    `json:"message"`
}

// JobSubmitRunInput preserves identity across manager-to-agent delivery retries.
type JobSubmitRunInput struct {
	RunID string `json:"runId"`
}

type JobListRunsInput struct {
	ID    string `path:"id"`
	JobID string `path:"jobId" minLength:"1"`
	Page  int    `query:"page" default:"1" minimum:"1"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100"`
}
type JobRunInput struct {
	ID    string `path:"id"`
	JobID string `path:"jobId" minLength:"1"`
	RunID string `path:"runId" format:"uuid"`
}
type (
	JobRunOutput      struct{ Body SchedulerRun }
	JobListRunsOutput struct{ Body SchedulerRunList }
)
