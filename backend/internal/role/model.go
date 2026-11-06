package role

import "getarcane.dev/arcane/v3/internal/database"

type (
	Role struct {
		database.BaseModel

		Name        string               `json:"name" gorm:"column:name;not null;uniqueIndex" sortable:"true"`
		Description *string              `json:"description,omitempty" gorm:"column:description"`
		Permissions database.StringSlice `json:"permissions" gorm:"column:permissions;type:text;not null"`
		BuiltIn     bool                 `json:"builtIn" gorm:"column:built_in;not null;default:false" sortable:"true"`
	}

	UserRoleAssignment struct {
		database.BaseModel

		UserID        string  `json:"userId" gorm:"column:user_id;not null;index"`
		RoleID        string  `json:"roleId" gorm:"column:role_id;not null;index"`
		EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id;index"`
		Source        string  `json:"source" gorm:"column:source;not null;default:'manual'"`
	}

	ApiKeyPermission struct {
		database.BaseModel

		EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id"`

		ApiKeyID   string `json:"apiKeyId" gorm:"column:api_key_id;not null;index"`
		Permission string `json:"permission" gorm:"column:permission;not null"`
	}

	OidcRoleMapping struct {
		database.BaseModel

		ClaimValue    string  `json:"claimValue" gorm:"column:claim_value;not null;index"`
		RoleID        string  `json:"roleId" gorm:"column:role_id;not null;index"`
		EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id"`
		Source        string  `json:"source" gorm:"column:source;not null;default:'manual'"`
	}
)

const (
	RoleAssignmentSourceManual = "manual"
	RoleAssignmentSourceOidc   = "oidc"
	OidcMappingSourceManual    = "manual"
	OidcMappingSourceEnv       = "env"
)

func (Role) TableName() string { return "roles" }

func (UserRoleAssignment) TableName() string { return "user_role_assignments" }

func (ApiKeyPermission) TableName() string { return "api_key_permissions" }

func (OidcRoleMapping) TableName() string { return "oidc_role_mappings" }
