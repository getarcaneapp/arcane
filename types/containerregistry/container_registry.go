package containerregistry

import "time"

type Response struct {
	// ID of the container registry.
	//
	// Required: true
	ID string `json:"id"`

	// URL of the container registry.
	//
	// Required: true
	URL string `json:"url"`

	// Username for authentication with the container registry.
	//
	// Required: true
	Username string `json:"username"`

	// Description of the container registry.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Insecure indicates if the registry uses an insecure connection (HTTP).
	//
	// Required: true
	Insecure bool `json:"insecure"`

	// Enabled indicates if the registry is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// CreatedAt is the date and time at which the registry was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the registry was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

type Sync struct {
	// ID of the container registry.
	//
	// Required: true
	ID string `json:"id" binding:"required"`

	// URL of the container registry.
	//
	// Required: true
	URL string `json:"url" binding:"required"`

	// Username for authentication with the container registry.
	//
	// Required: true
	Username string `json:"username" binding:"required"`

	// Token for authentication with the container registry.
	//
	// Required: true
	Token string `json:"token" binding:"required"`

	// Description of the container registry.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Insecure indicates if the registry uses an insecure connection (HTTP).
	//
	// Required: true
	Insecure bool `json:"insecure"`

	// Enabled indicates if the registry is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// CreatedAt is the date and time at which the registry was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the registry was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

type Credential struct {
	// URL of the container registry.
	//
	// Required: true
	URL string `json:"url" binding:"required"`

	// Username for authentication with the container registry.
	//
	// Required: true
	Username string `json:"username" binding:"required"`

	// Token for authentication with the container registry.
	//
	// Required: true
	Token string `json:"token" binding:"required"`

	// Enabled indicates if the credential is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`
}

type SyncRequest struct {
	// Registries is a list of container registries to sync.
	//
	// Required: true
	Registries []Sync `json:"registries" binding:"required"`
}
