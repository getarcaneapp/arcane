package dto

import "time"

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

type PasswordChangeRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

type LoginResponseDto struct {
	Token        string          `json:"token"`
	RefreshToken string          `json:"refreshToken"`
	ExpiresAt    time.Time       `json:"expiresAt"`
	User         UserResponseDto `json:"user"`
}

type TokenResponseDto struct {
	Token        string    `json:"token"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type MessageResponseDto struct {
	Message string `json:"message"`
}
