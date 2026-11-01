package types

import (
	"time"
)

type (
	// JobScheduleConfig contains cron schedules for Arcane background jobs.
	JobScheduleConfig struct {
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
	JobStatus struct {
		CurrentRun     *SchedulerRun          `json:"currentRun,omitempty"`
		LastRun        *SchedulerRun          `json:"lastRun,omitempty"`
		LastSuccess    *time.Time             `json:"lastSuccess,omitempty"`
		WorkerHealth   *SchedulerWorkerHealth `json:"workerHealth,omitempty"`
		NextRun        *time.Time             `json:"nextRun,omitempty"`
		LastError      string                 `json:"lastError,omitempty"`
		ID             string                 `json:"id"`
		Name           string                 `json:"name"`
		Description    string                 `json:"description"`
		Category       string                 `json:"category"`
		Schedule       string                 `json:"schedule"`
		SettingsKey    string                 `json:"settingsKey,omitempty"`
		Children       []JobStatus            `json:"children,omitempty"`
		Prerequisites  []JobPrerequisite      `json:"prerequisites"`
		Enabled        bool                   `json:"enabled"`
		ManagerOnly    bool                   `json:"managerOnly"`
		IsContinuous   bool                   `json:"isContinuous"`
		CanRunManually bool                   `json:"canRunManually"`
	}

	// JobPrerequisite represents a requirement that must be met for a job to run.
	JobPrerequisite struct {
		SettingKey string `json:"settingKey"`
		Label      string `json:"label"`
		IsMet      bool   `json:"isMet"`
	}

	// JobListResponse contains all jobs and system mode information.
	JobListResponse struct {
		ObservedAt  time.Time   `json:"observedAt"`
		Jobs        []JobStatus `json:"jobs"`
		DurableRuns bool        `json:"durableRuns"`
		Offline     bool        `json:"offline,omitempty"`
		IsAgent     bool        `json:"isAgent"`
	}

	// JobRunResponse is the response after manually triggering a job.
	JobRunResponse struct {
		RunID   string    `json:"runId,omitempty"`
		Status  RunStatus `json:"status,omitempty"`
		Message string    `json:"message"`
		Success bool      `json:"success"`
	}

	// JobSubmitRunInput preserves identity across manager-to-agent delivery retries.
	JobSubmitRunInput struct {
		RunID string `json:"runId"`
	}

	JobListRunsInput struct {
		ID    string `path:"id"`
		JobID string `path:"jobId" minLength:"1"`
		Page  int    `query:"page" default:"1" minimum:"1"`
		Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100"`
	}

	JobRunInput struct {
		ID    string `path:"id"`
		JobID string `path:"jobId" minLength:"1"`
		RunID string `path:"runId" format:"uuid"`
	}

	JobRunOutput      struct{ Body SchedulerRun }
	JobListRunsOutput struct{ Body SchedulerRunList }
)
