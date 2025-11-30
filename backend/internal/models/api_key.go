package models

import (
	"time"
)

type ApiKey struct {
	Name        string     `json:"name" gorm:"column:name;not null" sortable:"true"`
	Description *string    `json:"description,omitempty" gorm:"column:description"`
	KeyHash     string     `json:"-" gorm:"column:key_hash;not null"`
	KeyPrefix   string     `json:"keyPrefix" gorm:"column:key_prefix;not null"`
	UserID      string     `json:"userId" gorm:"column:user_id;not null"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty" gorm:"column:expires_at" sortable:"true"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty" gorm:"column:last_used_at" sortable:"true"`
	BaseModel
}

func (ApiKey) TableName() string {
	return "api_keys"
}
