package apikey

import "time"

// Create represents the request body for creating an API key.
type CreateApiKey struct {
	Name        string     `json:"name" minLength:"1" maxLength:"255" doc:"Name of the API key" example:"My API Key"`
	Description *string    `json:"description,omitempty" maxLength:"1000" doc:"Optional description of the API key"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty" doc:"Optional expiration date for the API key"`
}

// ApiKey represents an API key without the secret.
type ApiKey struct {
	ID          string     `json:"id" doc:"Unique identifier of the API key"`
	Name        string     `json:"name" doc:"Name of the API key"`
	Description *string    `json:"description,omitempty" doc:"Description of the API key"`
	KeyPrefix   string     `json:"keyPrefix" doc:"Prefix of the API key for identification"`
	UserID      string     `json:"userId" doc:"ID of the user who owns the API key"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty" doc:"Expiration date of the API key"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty" doc:"Last time the API key was used"`
	CreatedAt   time.Time  `json:"createdAt" doc:"Creation timestamp"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty" doc:"Last update timestamp"`
}

// ApiKeyCreatedDto represents a newly created API key with the full secret.
type ApiKeyCreatedDto struct {
	ApiKey
	Key string `json:"key" doc:"The full API key secret (only shown once)"`
}

// Update represents the request body for updating an API key.
type UpdateApiKey struct {
	Name        *string    `json:"name,omitempty" maxLength:"255" doc:"New name for the API key"`
	Description *string    `json:"description,omitempty" maxLength:"1000" doc:"New description for the API key"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty" doc:"New expiration date for the API key"`
}
