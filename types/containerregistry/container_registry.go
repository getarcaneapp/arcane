package containerregistry

import "time"

// Registry represents a container registry in API responses.
type ContainerRegistry struct {
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

	// RegistryType indicates the type of registry (generic, ecr).
	//
	// Required: true
	RegistryType string `json:"registryType"`

	// AWSAccessKeyID is the AWS Access Key ID for ECR registries.
	//
	// Required: false
	AWSAccessKeyID string `json:"awsAccessKeyId,omitempty"`

	// AWSRegion is the AWS region for ECR registries.
	//
	// Required: false
	AWSRegion string `json:"awsRegion,omitempty"`

	// CreatedAt is the date and time at which the registry was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the registry was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

type PullUsageResponse struct {
	// Registries contains pull usage visibility by configured registry.
	//
	// Required: true
	Registries []PullUsage `json:"registries"`
}

type PullUsage struct {
	// RegistryID is the configured registry row ID.
	//
	// Required: true
	RegistryID string `json:"registryId"`

	// Provider is the registry provider identifier when known.
	//
	// Required: true
	Provider string `json:"provider"`

	// Registry is the normalized registry host.
	//
	// Required: true
	Registry string `json:"registry"`

	// DisplayName is the human-readable registry name.
	//
	// Required: true
	DisplayName string `json:"displayName"`

	// Repository is the repository used for an optional rate limit probe.
	//
	// Required: false
	Repository string `json:"repository,omitempty"`

	// Limit is the pull limit for the current window.
	//
	// Required: false
	Limit *int `json:"limit,omitempty"`

	// Remaining is the remaining pulls for the current window.
	//
	// Required: false
	Remaining *int `json:"remaining,omitempty"`

	// Used is the number of pulls used in the current window.
	//
	// Required: false
	Used *int `json:"used,omitempty"`

	// WindowSeconds is the current rate limit window duration in seconds.
	//
	// Required: false
	WindowSeconds *int `json:"windowSeconds,omitempty"`

	// ObservedPulls is the number of successful pulls Arcane has initiated for this registry.
	//
	// Required: true
	ObservedPulls int64 `json:"observedPulls"`

	// AuthMethod is the authentication method used for probing.
	//
	// Required: true
	AuthMethod string `json:"authMethod"`

	// AuthUsername is the username used when probing with credentials.
	//
	// Required: false
	AuthUsername string `json:"authUsername,omitempty"`

	// Source is the registry-reported source for the rate limit bucket.
	//
	// Required: false
	Source string `json:"source,omitempty"`

	// CheckedAt is the time when usage was computed.
	//
	// Required: true
	CheckedAt time.Time `json:"checkedAt"`

	// Error contains a recoverable probe or counter error.
	//
	// Required: false
	Error string `json:"error,omitempty"`
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
	Username string `json:"username"`

	// Token for authentication with the container registry.
	//
	// Required: true
	Token string `json:"token"`

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

	// RegistryType indicates the type of registry (generic, ecr).
	//
	// Required: true
	RegistryType string `json:"registryType"`

	// AWSAccessKeyID is the AWS Access Key ID for ECR registries.
	//
	// Required: false
	AWSAccessKeyID string `json:"awsAccessKeyId,omitempty"`

	// AWSSecretAccessKey is the AWS Secret Access Key for ECR registries.
	// Sent decrypted between manager and agent for sync purposes.
	//
	// Required: false
	AWSSecretAccessKey string `json:"awsSecretAccessKey,omitempty"`

	// AWSRegion is the AWS region for ECR registries.
	//
	// Required: false
	AWSRegion string `json:"awsRegion,omitempty"`

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
