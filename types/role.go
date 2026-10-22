package types

import (
	"time"
)

// Role represents a named permission set returned by the API.
type Role struct {
	ID                string     `json:"id" readOnly:"true" doc:"Unique identifier of the role" example:"role_admin"`
	Name              string     `json:"name" required:"false" minLength:"1" maxLength:"100" doc:"Display name of the role" example:"Admin" unorm:"nfc" trim:"true"`
	Description       *string    `json:"description,omitempty" maxLength:"500" doc:"Optional human description" unorm:"nfc" trim:"true"`
	Permissions       []string   `json:"permissions" required:"false" doc:"Permission strings granted by this role" example:"[\"containers:start\",\"projects:deploy\"]"`
	BuiltIn           bool       `json:"builtIn" readOnly:"true" doc:"True for built-in roles (Admin/Editor/Deployer/Viewer); built-ins cannot be edited or deleted"`
	AssignedUserCount int        `json:"assignedUserCount" readOnly:"true" doc:"How many users currently hold an assignment to this role"`
	CreatedAt         time.Time  `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
	UpdatedAt         *time.Time `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
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

// SetUserRoleAssignments replaces every manual role assignment for one user. OIDC-
// sourced assignments are not affected and are managed via OIDC role mappings.
type SetUserRoleAssignments struct {
	Assignments []UserRoleAssignmentInput `json:"assignments" doc:"Desired manual role assignments for the user"`
}

// UserRoleAssignmentInput is one row in a SetUserRoleAssignments request.
type UserRoleAssignmentInput struct {
	RoleID        string  `json:"roleId" doc:"ID of the role to grant"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
}

// OidcRoleMapping maps an OIDC group/claim value to a role assignment. On
// every OIDC login, mappings whose ClaimValue is present in the user's
// configured groups claim are converted into source='oidc' role assignments.
type OidcRoleMapping struct {
	ID            string     `json:"id" readOnly:"true" doc:"Unique identifier of the mapping"`
	ClaimValue    string     `json:"claimValue" required:"false" doc:"OIDC claim value that triggers this mapping" example:"docker-admins"`
	RoleID        string     `json:"roleId" required:"false" doc:"Role to assign when the claim matches"`
	EnvironmentID *string    `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
	Source        string     `json:"source" readOnly:"true" enum:"manual,env" doc:"Origin: manual rows are editable via UI/API; env rows come from ARCANE_OIDC_ROLE_MAPPINGS and are read-only"`
	CreatedAt     time.Time  `json:"createdAt" readOnly:"true"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty" readOnly:"true"`
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

// PermissionsManifest describes every permission the server recognizes,
// grouped by resource. The frontend uses this to render the permission
// picker without hard-coding the taxonomy.
type PermissionsManifest struct {
	Resources      []PermissionResource `json:"resources" doc:"Resource groups, in display order"`
	Presets        []PermissionPreset   `json:"presets,omitempty" doc:"Optional preset permission bundles for bulk selection in the UI"`
	AccessSurfaces []AccessSurface      `json:"accessSurfaces,omitempty" doc:"Backend-owned route, landing, and category access metadata for frontend UX gating"`
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

// AccessSurface describes one UI surface whose visibility is driven by
// backend-owned RBAC metadata. It is advisory UX metadata; backend handlers and
// middleware remain authoritative for actual enforcement.
type AccessSurface struct {
	ID            string   `json:"id" doc:"Stable surface identifier" example:"settings.category.webhooks"`
	Kind          string   `json:"kind" enum:"route,settings-category,customize-category,landing" doc:"Surface type"`
	URL           string   `json:"url,omitempty" doc:"Route URL or prefix represented by this surface" example:"/settings/webhooks"`
	Label         string   `json:"label" doc:"Human-readable surface label" example:"Webhooks"`
	AccessMode    string   `json:"accessMode" enum:"permissions,any-child" doc:"How reachability is evaluated"`
	MatchMode     string   `json:"matchMode" enum:"any-of,all-of" doc:"How permissions are combined when accessMode is permissions"`
	ScopeMode     string   `json:"scopeMode" enum:"global-only,selected-env-plus-global,any-effective-scope" doc:"Which effective permission scope is considered"`
	Permissions   []string `json:"permissions,omitempty" doc:"Permissions used by permission-based surfaces"`
	Children      []string `json:"children,omitempty" doc:"Child surface IDs used by aggregate landing surfaces"`
	FallbackOrder int      `json:"fallbackOrder,omitempty" doc:"Positive ordering hint for route fallback selection"`
}
