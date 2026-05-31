package user

// CreateUser represents the request body for creating a new user.
// Role assignments are managed separately via PUT /users/{userId}/role-assignments.
type CreateUser struct {
	Username    string  `json:"username" minLength:"1" maxLength:"255" doc:"Username of the user" example:"johndoe"`
	Password    string  `json:"password" minLength:"8" doc:"Password of the user"`
	DisplayName *string `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user" example:"John Doe"`
	Email       *string `json:"email,omitempty" doc:"Email address of the user" example:"john@example.com"`
	Locale      *string `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
}

// UpdateUser represents the request body for updating a user.
// Role assignments are managed separately via PUT /users/{userId}/role-assignments.
type UpdateUser struct {
	Username    *string `json:"username,omitempty" minLength:"1" maxLength:"255" doc:"Username of the user"`
	DisplayName *string `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user"`
	Email       *string `json:"email,omitempty" doc:"Email address of the user"`
	Locale      *string `json:"locale,omitempty" doc:"Locale preference of the user"`
	Password    *string `json:"password,omitempty" minLength:"8" doc:"New password for the user"`
}

// RoleAssignmentSummary is a compact form of a user's role assignment used
// inside the User payload (so the frontend can render the assignments table
// without a separate request).
type RoleAssignmentSummary struct {
	RoleID        string  `json:"roleId" doc:"Role ID granted by this assignment"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID this assignment is scoped to; omit for a global assignment"`
	Source        string  `json:"source" doc:"How the assignment was created" enum:"manual,oidc"`
}

// User represents a user in API responses.
type User struct {
	ID                     string                  `json:"id" doc:"Unique identifier of the user" example:"550e8400-e29b-41d4-a716-446655440000"`
	Username               string                  `json:"username" doc:"Username of the user" example:"johndoe"`
	DisplayName            *string                 `json:"displayName,omitempty" doc:"Display name of the user" example:"John Doe"`
	Email                  *string                 `json:"email,omitempty" doc:"Email address of the user" example:"john@example.com"`
	RoleAssignments        []RoleAssignmentSummary `json:"roleAssignments" doc:"Role assignments held by the user"`
	PermissionsByEnv       map[string][]string     `json:"permissionsByEnv" doc:"Permissions the user effectively holds, keyed by environment ID. The 'global' key holds permissions that apply across every environment (and to org-level endpoints)."`
	CanDelete              bool                    `json:"canDelete" doc:"Whether the user can currently be deleted"`
	OidcSubjectId          *string                 `json:"oidcSubjectId,omitempty" doc:"OIDC subject identifier for SSO users"`
	Locale                 *string                 `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
	CreatedAt              string                  `json:"createdAt,omitempty" doc:"Date and time when the user was created"`
	UpdatedAt              string                  `json:"updatedAt,omitempty" doc:"Date and time when the user was last updated"`
	RequiresPasswordChange bool                    `json:"requiresPasswordChange" doc:"Whether the user must change their password"`
}
