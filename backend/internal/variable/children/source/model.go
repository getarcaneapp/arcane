package source

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
)

type (
	SecretSource struct {
		database.BaseModel

		LastFetchedAt        *time.Time                `gorm:"column:last_fetched_at"`
		Name                 string                    `gorm:"column:name" sortable:"true"`
		Kind                 string                    `gorm:"column:kind"`
		PresetID             string                    `gorm:"column:preset_id"`
		Definition           string                    `gorm:"column:definition"`
		Inputs               map[string]string         `gorm:"column:inputs;serializer:json"`
		Credentials          string                    `gorm:"column:credentials"`
		Path                 string                    `gorm:"column:path"`
		KeyPrefix            string                    `gorm:"column:key_prefix"`
		LastError            string                    `gorm:"column:last_error"`
		Environments         []environment.Environment `gorm:"many2many:secret_source_environments;joinForeignKey:SecretSourceID;joinReferences:EnvironmentID"`
		LastKeyCount         int                       `gorm:"column:last_key_count"`
		LastSkippedKeyCount  int                       `gorm:"column:last_skipped_key_count"`
		AllEnvironments      bool                      `gorm:"column:all_environments"`
		InjectIntoContainers bool                      `gorm:"column:inject_into_containers"`
		Enabled              bool                      `gorm:"column:enabled"`
	}
)

func (SecretSource) TableName() string { return "secret_sources" }
