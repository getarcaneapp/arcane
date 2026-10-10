package backup

import "github.com/getarcaneapp/arcane/backend/v2/internal/database"

// SystemBackupRecoveryConfig stores the encrypted recovery key; it must be reversible because Rustic needs the plaintext.
type SystemBackupRecoveryConfig struct {
	database.BaseModel

	EncryptedRecoveryKey string `gorm:"column:encrypted_recovery_key;type:text;not null"`
}

// TableName is the recovery key table.
func (SystemBackupRecoveryConfig) TableName() string { return "system_backup_recovery_config" }
