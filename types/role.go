package types

import (
	"time"
)

type (
	Role struct {
		CreatedAt         time.Time  `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
		Description       *string    `json:"description,omitempty" maxLength:"500" doc:"Optional human description" unorm:"nfc" trim:"true"`
		UpdatedAt         *time.Time `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
		ID                string     `json:"id" readOnly:"true" doc:"Unique identifier of the role" example:"role_admin"`
		Name              string     `json:"name" required:"false" minLength:"1" maxLength:"100" doc:"Display name of the role" example:"Admin" unorm:"nfc" trim:"true"`
		Permissions       []string   `json:"permissions" required:"false" doc:"Permission strings granted by this role" example:"[\"containers:start\",\"projects:deploy\"]"`
		AssignedUserCount int        `json:"assignedUserCount" readOnly:"true" doc:"How many users currently hold an assignment to this role"`
		BuiltIn           bool       `json:"builtIn" readOnly:"true" doc:"True for built-in roles (Admin/Editor/Deployer/Viewer); built-ins cannot be edited or deleted"`
	}

	RoleAssignment struct {
		CreatedAt     time.Time `json:"createdAt" doc:"Creation timestamp"`
		EnvironmentID *string   `json:"environmentId,omitempty" doc:"Environment ID this assignment is scoped to; omit for a global assignment"`
		ID            string    `json:"id" doc:"Unique identifier of the assignment"`
		UserID        string    `json:"userId" doc:"ID of the user holding this assignment"`
		RoleID        string    `json:"roleId" doc:"ID of the granted role"`
		Source        string    `json:"source" doc:"How the assignment was created" enum:"manual,oidc"`
	}

	SetUserRoleAssignments struct {
		Assignments []UserRoleAssignmentInput `json:"assignments" doc:"Desired manual role assignments for the user"`
	}

	UserRoleAssignmentInput struct {
		EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
		RoleID        string  `json:"roleId" doc:"ID of the role to grant"`
	}

	OidcRoleMapping struct {
		CreatedAt     time.Time  `json:"createdAt" readOnly:"true"`
		EnvironmentID *string    `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
		UpdatedAt     *time.Time `json:"updatedAt,omitempty" readOnly:"true"`
		ID            string     `json:"id" readOnly:"true" doc:"Unique identifier of the mapping"`
		ClaimValue    string     `json:"claimValue" required:"false" doc:"OIDC claim value that triggers this mapping" example:"docker-admins"`
		RoleID        string     `json:"roleId" required:"false" doc:"Role to assign when the claim matches"`
		Source        string     `json:"source" readOnly:"true" enum:"manual,env" doc:"Origin: manual rows are editable via UI/API; env rows come from ARCANE_OIDC_ROLE_MAPPINGS and are read-only"`
	}

	OidcRoleMappingSpec struct {
		EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the assignment to; omit for a global assignment"`
		ClaimValue    string  `json:"claimValue" doc:"OIDC claim value to match"`
		RoleID        string  `json:"roleId" doc:"Role ID to assign when the claim matches"`
	}

	PermissionsManifest struct {
		Resources      []PermissionResource `json:"resources" doc:"Resource groups, in display order"`
		Presets        []PermissionPreset   `json:"presets,omitempty" doc:"Optional preset permission bundles for bulk selection in the UI"`
		AccessSurfaces []AccessSurface      `json:"accessSurfaces,omitempty" doc:"Backend-owned route, landing, and category access metadata for frontend UX gating"`
	}

	PermissionResource struct {
		Key         string             `json:"key" doc:"Stable resource key" example:"containers"`
		Label       string             `json:"label" doc:"Human-readable label" example:"Containers"`
		Description string             `json:"description" doc:"What the resource covers" example:"Docker containers"`
		Scope       string             `json:"scope" enum:"global,env" doc:"'global' for org-level perms; 'env' for per-environment perms"`
		Actions     []PermissionAction `json:"actions" doc:"Actions available on this resource"`
	}

	PermissionAction struct {
		Key         string   `json:"key" doc:"Action verb" example:"start"`
		Permission  string   `json:"permission" doc:"Fully-qualified permission string used in role definitions" example:"containers:start"`
		Label       string   `json:"label" doc:"Human-readable label" example:"Start"`
		Description string   `json:"description,omitempty" doc:"Optional longer description"`
		Requires    []string `json:"requires,omitempty" doc:"Every permission this permission transitively requires; the server adds them when a role or API key is saved"`
	}

	PermissionPreset struct {
		Key         string   `json:"key" doc:"Stable preset key" example:"editor"`
		Label       string   `json:"label" doc:"Human-readable preset label" example:"All permissions (non-admin)"`
		Description string   `json:"description,omitempty" doc:"Optional longer description for the preset"`
		Permissions []string `json:"permissions" doc:"Permissions included when the preset is selected"`
	}

	AccessSurface struct {
		ID            string   `json:"id" doc:"Stable surface identifier" example:"settings.category.webhooks"`
		Kind          string   `json:"kind" enum:"route,settings-category,landing" doc:"Surface type"`
		URL           string   `json:"url,omitempty" doc:"Route URL or prefix represented by this surface" example:"/system/webhooks"`
		Label         string   `json:"label" doc:"Human-readable surface label" example:"Webhooks"`
		AccessMode    string   `json:"accessMode" enum:"permissions,any-child" doc:"How reachability is evaluated"`
		MatchMode     string   `json:"matchMode" enum:"any-of,all-of" doc:"How permissions are combined when accessMode is permissions"`
		ScopeMode     string   `json:"scopeMode" enum:"global-only,env-plus-global,any-effective-scope" doc:"Which effective permission scope is considered"`
		Permissions   []string `json:"permissions,omitempty" doc:"Permissions used by permission-based surfaces"`
		Children      []string `json:"children,omitempty" doc:"Child surface IDs used by aggregate landing surfaces"`
		FallbackOrder int      `json:"fallbackOrder,omitempty" doc:"Positive ordering hint for route fallback selection"`
	}
)
