package storage

import (
	"github.com/getarcaneapp/arcane/backend/internal/models"
)

type modelRegistration struct {
	tableName string
	pkColumn  string
	newModel  func() any
	newSlice  func() any
}

var registeredModels = []modelRegistration{
	{
		tableName: "settings",
		pkColumn:  "key",
		newModel:  func() any { return &models.SettingVariable{} },
		newSlice:  func() any { return &[]models.SettingVariable{} },
	},
	{
		tableName: "kv",
		pkColumn:  "key",
		newModel:  func() any { return &models.KVEntry{} },
		newSlice:  func() any { return &[]models.KVEntry{} },
	},
	{
		tableName: "users",
		pkColumn:  "id",
		newModel:  func() any { return &models.User{} },
		newSlice:  func() any { return &[]models.User{} },
	},
	{
		tableName: "api_keys",
		pkColumn:  "id",
		newModel:  func() any { return &models.ApiKey{} },
		newSlice:  func() any { return &[]models.ApiKey{} },
	},
	{
		tableName: "environments",
		pkColumn:  "id",
		newModel:  func() any { return &models.Environment{} },
		newSlice:  func() any { return &[]models.Environment{} },
	},
	{
		tableName: "container_registries",
		pkColumn:  "id",
		newModel:  func() any { return &models.ContainerRegistry{} },
		newSlice:  func() any { return &[]models.ContainerRegistry{} },
	},
	{
		tableName: "notification_settings",
		pkColumn:  "id",
		newModel:  func() any { return &models.NotificationSettings{} },
		newSlice:  func() any { return &[]models.NotificationSettings{} },
	},
	{
		tableName: "notification_logs",
		pkColumn:  "id",
		newModel:  func() any { return &models.NotificationLog{} },
		newSlice:  func() any { return &[]models.NotificationLog{} },
	},
	{
		tableName: "apprise_settings",
		pkColumn:  "id",
		newModel:  func() any { return &models.AppriseSettings{} },
		newSlice:  func() any { return &[]models.AppriseSettings{} },
	},
	{
		tableName: "git_repositories",
		pkColumn:  "id",
		newModel:  func() any { return &models.GitRepository{} },
		newSlice:  func() any { return &[]models.GitRepository{} },
	},
	{
		tableName: "projects",
		pkColumn:  "id",
		newModel:  func() any { return &models.Project{} },
		newSlice:  func() any { return &[]models.Project{} },
	},
	{
		tableName: "gitops_syncs",
		pkColumn:  "id",
		newModel:  func() any { return &models.GitOpsSync{} },
		newSlice:  func() any { return &[]models.GitOpsSync{} },
	},
	{
		tableName: "template_registries",
		pkColumn:  "id",
		newModel:  func() any { return &models.TemplateRegistry{} },
		newSlice:  func() any { return &[]models.TemplateRegistry{} },
	},
	{
		tableName: "compose_templates",
		pkColumn:  "id",
		newModel:  func() any { return &models.ComposeTemplate{} },
		newSlice:  func() any { return &[]models.ComposeTemplate{} },
	},
	{
		tableName: "events",
		pkColumn:  "id",
		newModel:  func() any { return &models.Event{} },
		newSlice:  func() any { return &[]models.Event{} },
	},
	{
		tableName: "image_builds",
		pkColumn:  "id",
		newModel:  func() any { return &models.ImageBuild{} },
		newSlice:  func() any { return &[]models.ImageBuild{} },
	},
	{
		tableName: "image_updates",
		pkColumn:  "id",
		newModel:  func() any { return &models.ImageUpdateRecord{} },
		newSlice:  func() any { return &[]models.ImageUpdateRecord{} },
	},
	{
		tableName: "auto_update_records",
		pkColumn:  "id",
		newModel:  func() any { return &models.AutoUpdateRecord{} },
		newSlice:  func() any { return &[]models.AutoUpdateRecord{} },
	},
	{
		tableName: "volume_backups",
		pkColumn:  "id",
		newModel:  func() any { return &models.VolumeBackup{} },
		newSlice:  func() any { return &[]models.VolumeBackup{} },
	},
	{
		tableName: "vulnerability_ignores",
		pkColumn:  "id",
		newModel:  func() any { return &models.VulnerabilityIgnore{} },
		newSlice:  func() any { return &[]models.VulnerabilityIgnore{} },
	},
	{
		tableName: "vulnerability_scans",
		pkColumn:  "id",
		newModel:  func() any { return &models.VulnerabilityScanRecord{} },
		newSlice:  func() any { return &[]models.VulnerabilityScanRecord{} },
	},
}
