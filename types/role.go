package types

import (
	"time"
)

// Role represents a named permission set returned by the API.
type Role struct {
	ID           string     `json:"id" readOnly:"true" doc:"Unique identifier of the role" example:"role_admin"`
	Name         string     `json:"name" required:"false" minLength:"1" maxLength:"100" doc:"Display name of the role" example:"Admin" unorm:"nfc" trim:"true"`
	Description  *string    `json:"description,omitempty" maxLength:"500" doc:"Optional human description" unorm:"nfc" trim:"true"`
	Permissions  []string   `json:"permissions" required:"false" doc:"Permission strings granted by this role" example:"[\"containers:start\",\"projects:deploy\"]"`
	BuiltIn      bool       `json:"builtIn" readOnly:"true" doc:"True for built-in roles, which are defined in code and cannot be edited or deleted"`
	BindingCount int        `json:"bindingCount" readOnly:"true" doc:"How many bindings reference this role"`
	CreatedAt    time.Time  `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
	UpdatedAt    *time.Time `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
}

type RoleBinding struct {
	ID              string            `json:"id" readOnly:"true" doc:"Unique identifier of the binding"`
	SubjectType     string            `json:"subjectType" enum:"user,group" doc:"Kind of subject the role is bound to"`
	SubjectID       string            `json:"subjectId" doc:"ID of the bound user or group"`
	SubjectName     string            `json:"subjectName" doc:"Username or group name of the subject"`
	RoleID          string            `json:"roleId" doc:"ID of the bound role"`
	RoleName        string            `json:"roleName" doc:"Display name of the bound role"`
	EnvironmentID   *string           `json:"environmentId,omitempty" doc:"Environment the binding applies to; omitted for a global binding"`
	EnvironmentName string            `json:"environmentName,omitempty" doc:"Display name of the environment"`
	Selector        *ResourceSelector `json:"selector,omitempty" doc:"Resource selector restricting the binding; omitted for unrestricted bindings"`
	Source          string            `json:"source" enum:"manual,federated,oidc" doc:"How the binding was created"`
	CreatedBy       *string           `json:"createdBy,omitempty" doc:"ID of the user who created the binding"`
	CreatedAt       time.Time         `json:"createdAt" doc:"Creation timestamp"`
}

type RoleBindingSummary struct {
	ID            string            `json:"id" doc:"Unique identifier of the binding"`
	RoleID        string            `json:"roleId" doc:"ID of the bound role"`
	RoleName      string            `json:"roleName" doc:"Display name of the bound role"`
	EnvironmentID *string           `json:"environmentId,omitempty" doc:"Environment the binding applies to; omitted for a global binding"`
	Selector      *ResourceSelector `json:"selector,omitempty" doc:"Resource selector restricting the binding"`
	Source        string            `json:"source" enum:"manual,federated,oidc" doc:"How the binding was created"`
	GroupID       *string           `json:"groupId,omitempty" doc:"Group the binding is inherited from; omitted for direct bindings"`
	GroupName     *string           `json:"groupName,omitempty" doc:"Name of the group the binding is inherited from"`
}

type CreateRoleBinding struct {
	SubjectType   string            `json:"subjectType" enum:"user,group" doc:"Kind of subject to bind"`
	SubjectID     string            `json:"subjectId" minLength:"1" doc:"ID of the user or group to bind"`
	RoleID        string            `json:"roleId" minLength:"1" doc:"ID of the role to bind"`
	EnvironmentID *string           `json:"environmentId,omitempty" doc:"Environment to scope the binding to; omit for a global binding"`
	Selector      *ResourceSelector `json:"selector,omitempty" doc:"Resource selector restricting the binding"`
}

type CreateRoleBindingResult struct {
	Binding            RoleBinding `json:"binding" doc:"The created binding"`
	DroppedPermissions []string    `json:"droppedPermissions" doc:"Role permissions the selector cannot scope and that the binding therefore does not grant"`
}

type RoleBindingFilter struct {
	SubjectType   string `json:"subjectType,omitempty"`
	SubjectID     string `json:"subjectId,omitempty"`
	RoleID        string `json:"roleId,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
}

type BindingRef struct {
	ID           string  `json:"id" doc:"Binding ID"`
	RoleID       string  `json:"roleId" doc:"Role granted by the binding"`
	RoleName     string  `json:"roleName" doc:"Display name of the role"`
	GroupID      *string `json:"groupId,omitempty" doc:"Group the binding is inherited from"`
	GroupName    *string `json:"groupName,omitempty" doc:"Name of the group the binding is inherited from"`
	Unrestricted bool    `json:"unrestricted" doc:"Whether the binding has no resource selector"`
}

type EffectivePermission struct {
	Permission   string             `json:"permission" doc:"Permission string"`
	Unrestricted bool               `json:"unrestricted" doc:"Whether the permission applies to every resource in scope"`
	Selectors    []ResourceSelector `json:"selectors,omitempty" doc:"Selectors limiting the permission when it is not unrestricted"`
	Via          []BindingRef       `json:"via" doc:"Bindings that grant the permission"`
}

type EffectiveScope struct {
	EnvironmentID *string               `json:"environmentId,omitempty" doc:"Environment the permissions apply to; omitted for global permissions"`
	Permissions   []EffectivePermission `json:"permissions" doc:"Permissions granted at this scope"`
}

type EffectiveAccess struct {
	UserID        string           `json:"userId" doc:"User the access was resolved for"`
	IsGlobalAdmin bool             `json:"isGlobalAdmin" doc:"Whether the user is an administrator"`
	Scopes        []EffectiveScope `json:"scopes" doc:"Permissions grouped by scope"`
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
	Key         string         `json:"key" doc:"Action verb" example:"start"`
	Permission  string         `json:"permission" doc:"Fully-qualified permission string used in role definitions" example:"containers:start"`
	Label       string         `json:"label" doc:"Human-readable label" example:"Start"`
	Description string         `json:"description,omitempty" doc:"Optional longer description"`
	Requires    []string       `json:"requires,omitempty" doc:"Permissions that should be auto-selected when this permission is chosen in the UI"`
	Selectors   []SelectorKind `json:"selectors,omitempty" doc:"Selector kinds that can scope this permission in a role binding"`
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
