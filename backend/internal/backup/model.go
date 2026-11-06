package backup

import "getarcane.dev/arcane/v3/internal/database"

type (
	SystemBackupRecoveryConfig struct {
		database.BaseModel

		EncryptedRecoveryKey string `gorm:"column:encrypted_recovery_key;type:text;not null"`
	}
)

func (SystemBackupRecoveryConfig) TableName() string { return "system_backup_recovery_config" }
