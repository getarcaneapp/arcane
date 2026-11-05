package enrollment

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
)

type (
	Token struct {
		database.BaseModel

		LastUsedAt    *time.Time `gorm:"column:last_used_at"`
		EnvironmentID *string    `gorm:"column:environment_id;uniqueIndex:idx_enrollment_tokens_environment_id,where:environment_id IS NOT NULL"`
		MaxUses       *int       `gorm:"column:max_uses"`

		Name           string `gorm:"column:name;not null"`
		TokenHash      string `gorm:"column:token_hash;not null;uniqueIndex"`
		TokenEncrypted string `gorm:"column:token_encrypted;not null"`
		UseCount       int    `gorm:"column:use_count;not null;default:0"`
	}
)

func (Token) TableName() string { return "enrollment_tokens" }
