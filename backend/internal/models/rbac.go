package models

// Role is a named permission set. Built-in roles (Admin, Editor, Deployer,
// Viewer) are seeded by migration 054 and cannot be edited or deleted.
type Role struct {
	BaseModel

	Name        string      `json:"name" gorm:"column:name;not null;uniqueIndex" sortable:"true"`
	Description *string     `json:"description,omitempty" gorm:"column:description"`
	Permissions StringSlice `json:"permissions" gorm:"column:permissions;type:text;not null"`
	BuiltIn     bool        `json:"builtIn" gorm:"column:built_in;not null;default:false" sortable:"true"`
}

func (Role) TableName() string { return "roles" }

// UserRoleAssignment binds a user to a role, optionally scoped to one
// environment. EnvironmentID == nil means "global" — the role's permissions
// apply across all environments AND to org-level resources.
//
// Source distinguishes manual assignments (managed by admins via the UI) from
// assignments synthesized from OIDC group mappings on every login.
type UserRoleAssignment struct {
	BaseModel

	UserID        string  `json:"userId" gorm:"column:user_id;not null;index"`
	RoleID        string  `json:"roleId" gorm:"column:role_id;not null;index"`
	EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id;index"`
	Source        string  `json:"source" gorm:"column:source;not null;default:'manual'"`
}

func (UserRoleAssignment) TableName() string { return "user_role_assignments" }

// Assignment source values stored in UserRoleAssignment.Source.
const (
	RoleAssignmentSourceManual = "manual"
	RoleAssignmentSourceOidc   = "oidc"
)

// ApiKeyPermission is one permission grant on an API key, optionally scoped to
// a single environment. Permissions are stored per-row (rather than as a JSON
// column on api_keys) so we can index by (api_key_id, permission) for fast
// lookups in the auth bridge.
type ApiKeyPermission struct {
	BaseModel

	ApiKeyID      string  `json:"apiKeyId" gorm:"column:api_key_id;not null;index"`
	Permission    string  `json:"permission" gorm:"column:permission;not null"`
	EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id"`
}

func (ApiKeyPermission) TableName() string { return "api_key_permissions" }

// OidcRoleMapping maps an OIDC group/claim value to a role assignment. On
// every OIDC login, the auth service replaces all source='oidc' rows on the
// user with assignments derived from the mappings whose ClaimValue matches a
// claim returned by the IdP.
//
// Source distinguishes UI/API-managed rows (the default) from env-declared
// rows reconciled at boot from OIDC_ROLE_MAPPINGS. Env-managed rows are
// read-only via the API — they can only be changed by editing the env var
// and restarting.
type OidcRoleMapping struct {
	BaseModel

	ClaimValue    string  `json:"claimValue" gorm:"column:claim_value;not null;index"`
	RoleID        string  `json:"roleId" gorm:"column:role_id;not null;index"`
	EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id"`
	Source        string  `json:"source" gorm:"column:source;not null;default:'manual'"`
}

func (OidcRoleMapping) TableName() string { return "oidc_role_mappings" }

const (
	OidcMappingSourceManual = "manual"
	OidcMappingSourceEnv    = "env"
)
