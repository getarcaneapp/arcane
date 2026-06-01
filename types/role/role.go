// Package role holds the public DTOs for Arcane's RBAC API: roles,
// per-user role assignments, OIDC group→role mappings, and the
// permission manifest exposed to the frontend.
package role

import "time"

// Role represents a named permission set returned by the API.
type Role struct {
	ID                string     `json:"id" doc:"Unique identifier of the role" example:"role_admin"`
	Name              string     `json:"name" doc:"Display name of the role" example:"Admin"`
	Description       *string    `json:"description,omitempty" doc:"Optional human description"`
	Permissions       []string   `json:"permissions" doc:"Permission strings granted by this role" example:"[\"containers:start\",\"projects:deploy\"]"`
	BuiltIn           bool       `json:"builtIn" doc:"True for built-in roles (Admin/Editor/Deployer/Viewer); built-ins cannot be edited or deleted"`
	AssignedUserCount int        `json:"assignedUserCount" doc:"How many users currently hold an assignment to this role"`
	CreatedAt         time.Time  `json:"createdAt" doc:"Creation timestamp"`
	UpdatedAt         *time.Time `json:"updatedAt,omitempty" doc:"Last update timestamp"`
}

// CreateRole is the request body for creating a custom role.
type CreateRole struct {
	Name        string   `json:"name" minLength:"1" maxLength:"100" doc:"Display name of the role" example:"Deploy Bot"`
	Description *string  `json:"description,omitempty" maxLength:"500" doc:"Optional human description"`
	Permissions []string `json:"permissions" minItems:"1" doc:"Permission strings granted by this role"`
}

// UpdateRole is the request body for editing a custom role. Built-in roles
// cannot be updated and will return 403.
type UpdateRole struct {
	Name        string   `json:"name" minLength:"1" maxLength:"100" doc:"Display name of the role"`
	Description *string  `json:"description,omitempty" maxLength:"500" doc:"Optional human description"`
	Permissions []string `json:"permissions" minItems:"1" doc:"Permission strings granted by this role"`
}

// RoleAssignment binds a user to a role, optionally scoped to one environment.
// EnvironmentID == nil means the assignment is global — it applies to every
// environment and to org-level endpoints.
type RoleAssignment struct {
	ID            string    `json:"id" doc:"Unique identifier of the assignment"`
	UserID        string    `json:"userId" doc:"ID of the user holding this assignment"`
	RoleID        string    `json:"roleId" doc:"ID of the granted role"`
	EnvironmentID *string   `json:"environmentId,omitempty" doc:"Environment ID this assignment is scoped to; omit for a global assignment"`
	Source        string    `json:"source" doc:"How the assignment was created" enum:"manual,oidc"`
	CreatedAt     time.Time `json:"createdAt" doc:"Creation timestamp"`
}

// SetUserAssignments replaces every manual role assignment for one user. OIDC-
// sourced assignments are not affected and are managed via OIDC role mappings.
type SetUserAssignments struct {
	Assignments []UserAssignmentInput `json:"assignments" doc:"Desired manual role assignments for the user"`
}

// UserAssignmentInput is one row in a SetUserAssignments request.
type UserAssignmentInput struct {
	RoleID        string  `json:"roleId" doc:"ID of the role to grant"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
}

// OidcRoleMapping maps an OIDC group/claim value to a role assignment. On
// every OIDC login, mappings whose ClaimValue is present in the user's
// configured groups claim are converted into source='oidc' role assignments.
type OidcRoleMapping struct {
	ID            string     `json:"id" doc:"Unique identifier of the mapping"`
	ClaimValue    string     `json:"claimValue" doc:"OIDC claim value that triggers this mapping" example:"docker-admins"`
	RoleID        string     `json:"roleId" doc:"Role to assign when the claim matches"`
	EnvironmentID *string    `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
	Source        string     `json:"source" enum:"manual,env" doc:"How this mapping was created. 'manual' rows are UI/API-managed and freely editable; 'env' rows are declared via OIDC_ROLE_MAPPINGS and are read-only at runtime."`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
}

// OidcRoleMappingSpec is the schema for one entry in the OIDC_ROLE_MAPPINGS
// env var. Operators can declare OIDC group→role mappings at deploy time;
// each spec is reconciled into a source='env' row at boot. Distinct from
// OidcRoleMapping (the API DTO) so the env-var format can evolve without
// breaking the API and vice versa.
type OidcRoleMappingSpec struct {
	ClaimValue    string  `json:"claimValue" doc:"OIDC claim value to match"`
	RoleID        string  `json:"roleId" doc:"Role ID to assign when the claim matches"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
}

// CreateOidcRoleMapping is the request body for adding a mapping.
type CreateOidcRoleMapping struct {
	ClaimValue    string  `json:"claimValue" minLength:"1" doc:"OIDC claim value to match"`
	RoleID        string  `json:"roleId" minLength:"1" doc:"Role to grant"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
}

// UpdateOidcRoleMapping is the request body for editing a mapping.
type UpdateOidcRoleMapping struct {
	ClaimValue    string  `json:"claimValue" minLength:"1"`
	RoleID        string  `json:"roleId" minLength:"1"`
	EnvironmentID *string `json:"environmentId,omitempty"`
}

// PermissionsManifest describes every permission the server recognizes,
// grouped by resource. The frontend uses this to render the permission
// picker without hard-coding the taxonomy.
type PermissionsManifest struct {
	Resources []PermissionResource `json:"resources" doc:"Resource groups, in display order"`
	Presets   []PermissionPreset   `json:"presets,omitempty" doc:"Optional preset permission bundles for bulk selection in the UI"`
}

// PermissionResource is one resource group in the manifest (e.g. "containers").
type PermissionResource struct {
	Key     string             `json:"key" doc:"Stable resource key" example:"containers"`
	Label   string             `json:"label" doc:"Human-readable label" example:"Containers"`
	Scope   string             `json:"scope" enum:"global,env" doc:"'global' for org-level perms; 'env' for per-environment perms"`
	Actions []PermissionAction `json:"actions" doc:"Actions available on this resource"`
}

// PermissionAction is one permission inside a resource group.
type PermissionAction struct {
	Key         string   `json:"key" doc:"Action verb" example:"start"`
	Permission  string   `json:"permission" doc:"Fully-qualified permission string used in role definitions" example:"containers:start"`
	Label       string   `json:"label" doc:"Human-readable label" example:"Start"`
	Description string   `json:"description,omitempty" doc:"Optional longer description"`
	Requires    []string `json:"requires,omitempty" doc:"Permissions that should be auto-selected when this permission is chosen in the UI"`
}

// PermissionPreset is an optional bulk-selection bundle exposed to the UI.
type PermissionPreset struct {
	Key         string   `json:"key" doc:"Stable preset key" example:"editor"`
	Label       string   `json:"label" doc:"Human-readable preset label" example:"All permissions (non-admin)"`
	Description string   `json:"description,omitempty" doc:"Optional longer description for the preset"`
	Permissions []string `json:"permissions" doc:"Permissions included when the preset is selected"`
}

// ApiKeyPermissionGrant is one permission grant on an API key, optionally
// scoped to a single environment. Used by the API key create/update flow.
type ApiKeyPermissionGrant struct {
	Permission    string  `json:"permission" doc:"Permission string to grant" example:"containers:list"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the grant to; omit for a global grant"`
}
