package models

import (
	"time"
)

const (
	// ApiKeyKindScoped keys carry their own permission grant rows.
	ApiKeyKindScoped = "scoped"
	// ApiKeyKindPersonal keys have no grants; they inherit the owner's role
	// permissions at authentication time.
	ApiKeyKindPersonal = "personal"
)

type ApiKey struct {
	BaseModel

	Name             string             `json:"name" gorm:"column:name;not null" sortable:"true"`
	Description      *string            `json:"description,omitempty" gorm:"column:description"`
	KeyHash          string             `json:"-" gorm:"column:key_hash;not null"`
	KeyPrefix        string             `json:"keyPrefix" gorm:"column:key_prefix;not null"`
	ManagedBy        *string            `json:"-" gorm:"column:managed_by"`
	Kind             string             `json:"kind" gorm:"column:kind;not null;default:scoped"`
	UserID           *string            `json:"userId,omitempty" gorm:"column:user_id"`
	EnvironmentID    *string            `json:"environmentId,omitempty" gorm:"column:environment_id"`
	ExpiresAt        *time.Time         `json:"expiresAt,omitempty" gorm:"column:expires_at" sortable:"true"`
	LastUsedAt       *time.Time         `json:"lastUsedAt,omitempty" gorm:"column:last_used_at" sortable:"true"`
	PermissionGrants []ApiKeyPermission `json:"-" gorm:"foreignKey:ApiKeyID;references:ID;constraint:OnDelete:CASCADE"`
}

func (ApiKey) TableName() string {
	return "api_keys"
}
