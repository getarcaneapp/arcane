package user

// CreateUser represents the request body for creating a new user.
type CreateUser struct {
	Username    string   `json:"username" minLength:"1" maxLength:"255" doc:"Username of the user" example:"johndoe"`
	Password    string   `json:"password" minLength:"8" doc:"Password of the user"`
	DisplayName *string  `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user" example:"John Doe"`
	Email       *string  `json:"email,omitempty" format:"email" doc:"Email address of the user" example:"john@example.com"`
	Roles       []string `json:"roles,omitempty" doc:"Roles assigned to the user" example:"[\"user\"]"`
	Locale      *string  `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
}

// UpdateUser represents the request body for updating a user.
type UpdateUser struct {
	DisplayName *string  `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user"`
	Email       *string  `json:"email,omitempty" format:"email" doc:"Email address of the user"`
	Roles       []string `json:"roles,omitempty" doc:"Roles assigned to the user"`
	Locale      *string  `json:"locale,omitempty" doc:"Locale preference of the user"`
	Password    *string  `json:"password,omitempty" minLength:"8" doc:"New password for the user"`
}

// User represents a user in API responses.
type User struct {
	ID                     string   `json:"id" doc:"Unique identifier of the user" example:"550e8400-e29b-41d4-a716-446655440000"`
	Username               string   `json:"username" doc:"Username of the user" example:"johndoe"`
	DisplayName            *string  `json:"displayName,omitempty" doc:"Display name of the user" example:"John Doe"`
	Email                  *string  `json:"email,omitempty" doc:"Email address of the user" example:"john@example.com"`
	Roles                  []string `json:"roles" doc:"Roles assigned to the user" example:"[\"user\", \"admin\"]"`
	OidcSubjectId          *string  `json:"oidcSubjectId,omitempty" doc:"OIDC subject identifier for SSO users"`
	Locale                 *string  `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
	CreatedAt              string   `json:"createdAt,omitempty" doc:"Date and time when the user was created"`
	UpdatedAt              string   `json:"updatedAt,omitempty" doc:"Date and time when the user was last updated"`
	RequiresPasswordChange bool     `json:"requiresPasswordChange" doc:"Whether the user must change their password"`
}
