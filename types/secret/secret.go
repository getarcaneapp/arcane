package secret

import "time"

// Secret represents a secret metadata record.
type Secret struct {
	// ID is the unique identifier of the secret.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the secret.
	//
	// Required: true
	Name string `json:"name"`

	// EnvironmentID is the environment this secret belongs to.
	//
	// Required: true
	EnvironmentID string `json:"environmentId"`

	// Description provides optional context for the secret.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// CreatedAt is the time the secret was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the time the secret was last updated.
	//
	// Required: false
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`

	// ComposePath is the absolute host path for file-based secrets.
	//
	// Required: true
	ComposePath string `json:"composePath"`
}

// SecretWithContent includes the decrypted content.
type SecretWithContent struct {
	Secret

	// Content is the decrypted secret value.
	//
	// Required: true
	Content string `json:"content"`
}

// Create represents the request to create a secret.
type Create struct {
	// Name of the secret.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// Content of the secret.
	//
	// Required: true
	Content string `json:"content" binding:"required"`

	// Description provides optional context for the secret.
	//
	// Required: false
	Description *string `json:"description,omitempty"`
}

// Update represents the request to update a secret.
type Update struct {
	// Name of the secret.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// Content of the secret.
	//
	// Required: false
	Content *string `json:"content,omitempty"`

	// Description provides optional context for the secret.
	//
	// Required: false
	Description *string `json:"description,omitempty"`
}

// Mount represents a secret mount request for containers.
type Mount struct {
	// SecretID is the secret identifier to mount.
	//
	// Required: true
	SecretID string `json:"secretId" binding:"required"`
}
