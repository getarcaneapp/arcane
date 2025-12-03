package user

type Create struct {
	// Username of the user.
	//
	// Required: true
	Username string `json:"username" binding:"required"`

	// Password of the user.
	//
	// Required: true
	Password string `json:"password" binding:"required"`

	// DisplayName of the user.
	//
	// Required: false
	DisplayName *string `json:"displayName,omitempty"`

	// Email address of the user.
	//
	// Required: false
	Email *string `json:"email,omitempty"`

	// Roles assigned to the user.
	//
	// Required: false
	Roles []string `json:"roles,omitempty"`

	// Locale of the user.
	//
	// Required: false
	Locale *string `json:"locale,omitempty"`
}

type Update struct {
	// DisplayName of the user.
	//
	// Required: false
	DisplayName *string `json:"displayName,omitempty"`

	// Email address of the user.
	//
	// Required: false
	Email *string `json:"email,omitempty"`

	// Roles assigned to the user.
	//
	// Required: false
	Roles []string `json:"roles,omitempty"`

	// Locale of the user.
	//
	// Required: false
	Locale *string `json:"locale,omitempty"`

	// Password of the user.
	//
	// Required: false
	Password *string `json:"password,omitempty"`
}

type Response struct {
	// ID of the user.
	//
	// Required: true
	ID string `json:"id"`

	// Username of the user.
	//
	// Required: true
	Username string `json:"username"`

	// DisplayName of the user.
	//
	// Required: false
	DisplayName *string `json:"displayName,omitempty"`

	// Email address of the user.
	//
	// Required: false
	Email *string `json:"email,omitempty"`

	// Roles assigned to the user.
	//
	// Required: true
	Roles []string `json:"roles"`

	// OidcSubjectId of the user.
	//
	// Required: false
	OidcSubjectId *string `json:"oidcSubjectId,omitempty"`

	// Locale of the user.
	//
	// Required: false
	Locale *string `json:"locale,omitempty"`

	// CreatedAt is the date and time at which the user was created.
	//
	// Required: false
	CreatedAt string `json:"createdAt,omitempty"`

	// UpdatedAt is the date and time at which the user was last updated.
	//
	// Required: false
	UpdatedAt string `json:"updatedAt,omitempty"`

	// RequiresPasswordChange indicates if the user is required to change their password.
	//
	// Required: true
	RequiresPasswordChange bool `json:"requiresPasswordChange"`
}
