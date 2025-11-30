package dto

import "time"

type CreateApiKeyDto struct {
	Name        string     `json:"name" binding:"required"`
	Description *string    `json:"description,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

type ApiKeyDto struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	KeyPrefix   string     `json:"keyPrefix"`
	UserID      string     `json:"userId"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
}

type ApiKeyCreatedDto struct {
	ApiKeyDto
	Key string `json:"key"`
}

type UpdateApiKeyDto struct {
	Name        *string    `json:"name,omitempty"`
	Description *string    `json:"description,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}
