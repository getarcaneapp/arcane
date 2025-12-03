package auth

type Login struct {
	// Username of the user.
	//
	// Required: true
	Username string `json:"username" binding:"required"`

	// Password of the user.
	//
	// Required: true
	Password string `json:"password" binding:"required"`
}

type Refresh struct {
	// RefreshToken is the refresh token used to obtain a new access token.
	//
	// Required: true
	RefreshToken string `json:"refreshToken" binding:"required"`
}

type PasswordChange struct {
	// CurrentPassword is the current password of the user.
	//
	// Required: false
	CurrentPassword string `json:"currentPassword"`

	// NewPassword is the new password for the user.
	//
	// Required: true
	NewPassword string `json:"newPassword" binding:"required"`
}
