package role

import (
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
)

// Role is a named permission set. Built-in roles (Admin, Editor, Deployer,
// Viewer) are seeded by migration 054 and cannot be edited or deleted.
type Role struct {
	database.BaseModel

	Name        string               `json:"name" gorm:"column:name;not null;uniqueIndex" sortable:"true"`
	Description *string              `json:"description,omitempty" gorm:"column:description"`
	Permissions database.StringSlice `json:"permissions" gorm:"column:permissions;type:text;not null"`
	BuiltIn     bool                 `json:"builtIn" gorm:"column:built_in;not null;default:false" sortable:"true"`
}

func (Role) TableName() string { return "roles" }

type RoleBinding struct {
	database.BaseModel

	UserID        *string                 `json:"userId,omitempty" gorm:"column:user_id;index"`
	GroupID       *string                 `json:"groupId,omitempty" gorm:"column:group_id;index"`
	RoleID        string                  `json:"roleId" gorm:"column:role_id;not null;index"`
	EnvironmentID *string                 `json:"environmentId,omitempty" gorm:"column:environment_id"`
	Selector      *types.ResourceSelector `json:"selector,omitempty" gorm:"column:selector;serializer:json"`
	SelectorHash  string                  `json:"selectorHash" gorm:"column:selector_hash;not null;default:''"`
	Source        string                  `json:"source" gorm:"column:source;not null;default:'manual'"`
	CreatedBy     *string                 `json:"createdBy,omitempty" gorm:"column:created_by"`
}

func (RoleBinding) TableName() string { return "role_bindings" }

const (
	BindingSourceManual    = "manual"
	BindingSourceFederated = "federated"
	BindingSourceOidc      = "oidc"
)

// ApiKeyPermission is one permission grant on an API key, optionally scoped to
// a single environment. Permissions are stored per-row (rather than as a JSON
// column on api_keys) so we can index by (api_key_id, permission) for fast
// lookups in the auth bridge.
type ApiKeyPermission struct {
	database.BaseModel

	ApiKeyID      string  `json:"apiKeyId" gorm:"column:api_key_id;not null;index"`
	Permission    string  `json:"permission" gorm:"column:permission;not null"`
	EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id"`
}

func (ApiKeyPermission) TableName() string { return "api_key_permissions" }
