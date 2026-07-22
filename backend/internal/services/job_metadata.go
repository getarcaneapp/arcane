package services

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	"github.com/getarcaneapp/arcane/types/v2/meta"
)

var jobMetadataRegistry = map[string]meta.JobMetadata{
	"environment-health": {
		ID:             "environment-health",
		Name:           "Environment Health",
		Description:    "Checks the health and connectivity of all enabled environments",
		Category:       "monitoring",
		SettingsKey:    "environmentHealthInterval",
		ManagerOnly:    true,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites:  []meta.JobPrerequisiteMetadata{},
	},
	"docker-client-refresh": {
		ID:             "docker-client-refresh",
		Name:           "Docker Client Refresh",
		Description:    "Refreshes the cached Docker client API version after daemon restarts or upgrades",
		Category:       "monitoring",
		SettingsKey:    "dockerClientRefreshInterval",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites:  []meta.JobPrerequisiteMetadata{},
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
		Prerequisites:  []meta.JobPrerequisiteMetadata{},
	},
	"expired-sessions-cleanup": {
		ID:             "expired-sessions-cleanup",
		Name:           "Expired Sessions Cleanup",
		Description:    "Removes expired and old revoked user sessions from the database",
		Category:       "maintenance",
		SettingsKey:    "expiredSessionsCleanupInterval",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites:  []meta.JobPrerequisiteMetadata{},
	},
	"analytics-heartbeat": {
		ID:             "analytics-heartbeat",
		Name:           "Analytics Heartbeat",
		Description:    "Checks hourly and sends anonymous telemetry at most once per 24 hours",
		Category:       "telemetry",
		SettingsKey:    "",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []meta.JobPrerequisiteMetadata{
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
		Prerequisites: []meta.JobPrerequisiteMetadata{
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
		Name:           "Image Update Watcher",
		Description:    "Checks container registries on a schedule and after Docker image changes",
		Category:       "updates",
		SettingsKey:    "pollingInterval",
		EnabledKey:     "pollingEnabled",
		ManagerOnly:    false,
		IsContinuous:   true,
		CanRunManually: true,
		Prerequisites: []meta.JobPrerequisiteMetadata{
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
		Prerequisites: []meta.JobPrerequisiteMetadata{
			{
				SettingKey:  "scheduledPruneEnabled",
				Label:       "Scheduled prune enabled",
				SettingsURL: "/settings/general",
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
		Prerequisites:  []meta.JobPrerequisiteMetadata{},
	},
	"vulnerability-scan": {
		ID:             "vulnerability-scan",
		Name:           "Vulnerability Scan",
		Description:    "Scans all Docker images for known vulnerabilities using Trivy",
		Category:       "security",
		SettingsKey:    "vulnerabilityScanInterval",
		EnabledKey:     "vulnerabilityScanEnabled",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []meta.JobPrerequisiteMetadata{
			{
				SettingKey:  "vulnerabilityScanEnabled",
				Label:       "Scheduled vulnerability scan enabled",
				SettingsURL: "/settings/security",
			},
		},
	},
	"auto-heal": {
		ID:             "auto-heal",
		Name:           "Auto Heal",
		Description:    "Automatically restarts containers that become unhealthy",
		Category:       "monitoring",
		SettingsKey:    "autoHealInterval",
		EnabledKey:     "autoHealEnabled",
		ManagerOnly:    false,
		IsContinuous:   false,
		CanRunManually: true,
		Prerequisites: []meta.JobPrerequisiteMetadata{
			{
				SettingKey:  "autoHealEnabled",
				Label:       "Auto heal enabled",
				SettingsURL: "/settings/general",
			},
		},
	},
}

func toJobStatusInternal(jobMeta meta.JobMetadata, schedule string, nextRun *time.Time, enabled bool, prerequisites []jobschedule.JobPrerequisite) jobschedule.JobStatus {
	return jobschedule.JobStatus{
		ID:             jobMeta.ID,
		Name:           jobMeta.Name,
		Description:    jobMeta.Description,
		Category:       jobMeta.Category,
		Schedule:       schedule,
		NextRun:        nextRun,
		Enabled:        enabled,
		ManagerOnly:    jobMeta.ManagerOnly,
		IsContinuous:   jobMeta.IsContinuous,
		CanRunManually: jobMeta.CanRunManually,
		Prerequisites:  prerequisites,
		SettingsKey:    jobMeta.SettingsKey,
	}
}
