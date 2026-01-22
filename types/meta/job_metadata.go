package meta

import (
	"time"

	"github.com/getarcaneapp/arcane/types/jobschedule"
)

type JobMetadata struct {
	ID             string
	Name           string
	Description    string
	Category       string
	SettingsKey    string
	EnabledKey     string
	ManagerOnly    bool
	IsContinuous   bool
	CanRunManually bool
	Prerequisites  []JobPrerequisiteMetadata
}

type JobPrerequisiteMetadata struct {
	SettingKey  string
	Label       string
	SettingsURL string
}

var jobMetadataRegistry = map[string]JobMetadata{
	"environment-health": {
		ID:             "environment-health",
		Name:           "Environment Health",
		Description:    "Checks the health and connectivity of all enabled environments",
		Category:       "monitoring",
		SettingsKey:    "environmentHealthInterval",
		ManagerOnly:    true,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites:  []JobPrerequisiteMetadata{},
	},
	"event-cleanup": {
		ID:             "event-cleanup",
		Name:           "Event Cleanup",
		Description:    "Removes old system events to maintain database performance",
		Category:       "maintenance",
		SettingsKey:    "eventCleanupInterval",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites:  []JobPrerequisiteMetadata{},
	},
	"analytics-heartbeat": {
		ID:             "analytics-heartbeat",
		Name:           "Analytics Heartbeat",
		Description:    "Sends usage statistics and telemetry data",
		Category:       "telemetry",
		SettingsKey:    "analyticsHeartbeatInterval",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []JobPrerequisiteMetadata{
			{
				SettingKey:  "analyticsEnabled",
				Label:       "Analytics enabled",
				SettingsURL: "/settings/general",
			},
		},
	},
	"auto-update": {
		ID:             "auto-update",
		Name:           "Auto Update",
		Description:    "Automatically updates containers when new images are available",
		Category:       "updates",
		SettingsKey:    "autoUpdateInterval",
		EnabledKey:     "autoUpdate",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []JobPrerequisiteMetadata{
			{
				SettingKey:  "pollingEnabled",
				Label:       "Image polling enabled",
				SettingsURL: "/settings/updates",
			},
			{
				SettingKey:  "autoUpdate",
				Label:       "Auto update enabled",
				SettingsURL: "/settings/updates",
			},
		},
	},
	"image-polling": {
		ID:             "image-polling",
		Name:           "Image Polling",
		Description:    "Checks container registries for new image versions",
		Category:       "updates",
		SettingsKey:    "pollingInterval",
		EnabledKey:     "pollingEnabled",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []JobPrerequisiteMetadata{
			{
				SettingKey:  "pollingEnabled",
				Label:       "Image polling enabled",
				SettingsURL: "/settings/updates",
			},
		},
	},
	"scheduled-prune": {
		ID:             "scheduled-prune",
		Name:           "Scheduled Prune",
		Description:    "Removes unused containers, images, volumes, and networks",
		Category:       "maintenance",
		SettingsKey:    "scheduledPruneInterval",
		EnabledKey:     "scheduledPruneEnabled",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []JobPrerequisiteMetadata{
			{
				SettingKey:  "scheduledPruneEnabled",
				Label:       "Scheduled prune enabled",
				SettingsURL: "/settings/general",
			},
		},
	},
	"gitops-sync": {
		ID:             "gitops-sync",
		Name:           "GitOps Sync",
		Description:    "Synchronizes project state with configured git repositories",
		Category:       "sync",
		SettingsKey:    "gitopsSyncInterval",
		EnabledKey:     "gitopsSyncEnabled",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []JobPrerequisiteMetadata{
			{
				SettingKey:  "gitopsSyncEnabled",
				Label:       "GitOps sync enabled",
				SettingsURL: "/settings/gitops",
			},
		},
	},
	"filesystem-watcher": {
		ID:             "filesystem-watcher",
		Name:           "Filesystem Watcher",
		Description:    "Monitors project directory for changes and syncs automatically",
		Category:       "sync",
		SettingsKey:    "",
		ManagerOnly:    false,
		IsContinuous:   true,
		CanRunManually: false,
		Prerequisites:  []JobPrerequisiteMetadata{},
	},
}

func GetJobMetadata(jobID string) (JobMetadata, bool) {
	meta, ok := jobMetadataRegistry[jobID]
	return meta, ok
}

func GetAllJobMetadata() map[string]JobMetadata {
	return jobMetadataRegistry
}

func (meta JobMetadata) ToJobStatus(schedule string, nextRun *time.Time, enabled bool, prerequisites []jobschedule.JobPrerequisite) jobschedule.JobStatus {
	return jobschedule.JobStatus{
		ID:             meta.ID,
		Name:           meta.Name,
		Description:    meta.Description,
		Category:       meta.Category,
		Schedule:       schedule,
		NextRun:        nextRun,
		Enabled:        enabled,
		ManagerOnly:    meta.ManagerOnly,
		IsContinuous:   meta.IsContinuous,
		CanRunManually: meta.CanRunManually,
		Prerequisites:  prerequisites,
		SettingsKey:    meta.SettingsKey,
	}
}
