package auth

import (
	"time"

	"github.com/getarcaneapp/arcane/types/user"
)

// Login represents the login request body.
type Login struct {
	Username string `json:"username" minLength:"1" maxLength:"255" doc:"Username of the user" example:"admin"`
	Password string `json:"password" minLength:"1" doc:"Password of the user"`
}

// Refresh represents the token refresh request body.
type Refresh struct {
	RefreshToken string `json:"refreshToken" minLength:"1" doc:"Refresh token used to obtain a new access token"`
}

// PasswordChange represents the password change request body.
type PasswordChange struct {
	CurrentPassword string `json:"currentPassword,omitempty" doc:"Current password of the user (required for non-OIDC users)"`
	NewPassword     string `json:"newPassword" minLength:"8" doc:"New password for the user"`
}

// LoginResponse represents the successful login response data.
type LoginResponse struct {
	Token        string        `json:"token" doc:"JWT access token"`
	RefreshToken string        `json:"refreshToken" doc:"Refresh token for obtaining new access tokens"`
	ExpiresAt    time.Time     `json:"expiresAt" doc:"Expiration time of the access token"`
	User         user.User `json:"user" doc:"Authenticated user information"`
}

// TokenRefreshResponse represents the successful token refresh response data.
type TokenRefreshResponse struct {
	Token        string    `json:"token" doc:"New JWT access token"`
	RefreshToken string    `json:"refreshToken" doc:"New refresh token"`
	ExpiresAt    time.Time `json:"expiresAt" doc:"Expiration time of the new access token"`
}
