package types

import (
	"time"
)

type (
	// ContainerRegistryRepository is an image repository stored in a container registry.
	ContainerRegistryRepository struct {
		// Name of the repository, without the registry host.
		//
		// Required: true
		Name string `json:"name"`
	}

	// ContainerRegistryRepositoryTag describes a tag of a repository and the manifest it points to.
	ContainerRegistryRepositoryTag struct {
		// Created is the creation date of the image, when a single platform is published.
		//
		// Required: false
		Created *time.Time `json:"created,omitempty"`

		// Name of the tag.
		//
		// Required: true
		Name string `json:"name"`

		// Digest of the manifest the tag points to.
		//
		// Required: false
		Digest string `json:"digest,omitempty"`

		// MediaType of the manifest the tag points to.
		//
		// Required: false
		MediaType string `json:"mediaType,omitempty"`

		// Error explains why the manifest details could not be loaded.
		//
		// Required: false
		Error string `json:"error,omitempty"`

		// Platforms published under the tag.
		//
		// Required: true
		Platforms []ContainerRegistryTagPlatform `json:"platforms"`

		// Size is the compressed size in bytes of every platform image.
		//
		// Required: true
		Size int64 `json:"size"`
	}

	// ContainerRegistryTagPlatform describes one platform image published under a tag.
	ContainerRegistryTagPlatform struct {
		// OS of the platform image.
		//
		// Required: true
		OS string `json:"os"`

		// Architecture of the platform image.
		//
		// Required: true
		Architecture string `json:"architecture"`

		// Variant of the platform architecture.
		//
		// Required: false
		Variant string `json:"variant,omitempty"`

		// Digest of the platform image manifest.
		//
		// Required: true
		Digest string `json:"digest"`

		// Size is the compressed size in bytes of the platform image.
		//
		// Required: true
		Size int64 `json:"size"`
	}

	// ContainerRegistryDeleteTagResponse reports the manifest removed from the registry.
	ContainerRegistryDeleteTagResponse struct {
		// Digest of the deleted manifest. Every tag pointing to it is removed.
		//
		// Required: true
		Digest string `json:"digest"`
	}

	// ContainerRegistry represents a container registry in API responses.
	ContainerRegistry struct {
		// CreatedAt is the date and time at which the registry was created.
		//
		// Required: true
		CreatedAt time.Time `json:"createdAt" readOnly:"true"`

		// UpdatedAt is the date and time at which the registry was last updated.
		//
		// Required: true
		UpdatedAt time.Time `json:"updatedAt" readOnly:"true"`

		// Description of the container registry.
		//
		// Required: false
		Description *string `json:"description,omitempty" unorm:"nfc" trim:"true"`

		// ID of the container registry.
		//
		// Required: true
		ID string `json:"id" readOnly:"true"`

		// URL of the container registry.
		//
		// Required: false
		URL string `json:"url" required:"false"`

		// Host is the registry host parsed from URL, used in image references.
		//
		// Required: true
		Host string `json:"host" readOnly:"true"`

		// Namespace is the repository namespace parsed from URL's path, if any.
		//
		// Required: true
		Namespace string `json:"namespace" readOnly:"true"`

		// Username for authentication with the container registry.
		//
		// Required: false
		Username string `json:"username" required:"false"`

		// RegistryType indicates the type of registry (generic, ecr).
		//
		// Required: false
		RegistryType string `json:"registryType" required:"false"`

		// AWSAccessKeyID is the AWS Access Key ID for ECR registries.
		// Empty means the default AWS credential chain (e.g. an IAM instance profile) is used.
		//
		// Required: false
		AWSAccessKeyID string `json:"awsAccessKeyId,omitempty"`

		// AWSRegion is the AWS region for ECR registries.
		//
		// Required: false
		AWSRegion string `json:"awsRegion,omitempty"`

		Token              string `json:"token,omitempty" writeOnly:"true" copier:"-"`
		AWSSecretAccessKey string `json:"awsSecretAccessKey,omitempty" writeOnly:"true" copier:"-"`

		// RepositoryNames is the list of pre-configured repository namespace
		// strings available for selection when pushing images from the build page.
		//
		// Required: false
		RepositoryNames []string `json:"repositoryNames" required:"false"`

		// Insecure indicates if the registry uses an insecure connection (HTTP).
		//
		// Required: false
		Insecure bool `json:"insecure" required:"false"`

		// Enabled indicates if the registry is enabled.
		//
		// Required: false
		Enabled bool `json:"enabled" required:"false"`
	}

	ContainerRegistryPullUsageResponse struct {
		// Registries contains pull usage visibility by configured registry.
		//
		// Required: true
		Registries []ContainerRegistryPullUsage `json:"registries"`
	}

	ContainerRegistryPullUsage struct {
		// CheckedAt is the time when usage was computed.
		//
		// Required: true
		CheckedAt time.Time `json:"checkedAt"`

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

		// Error contains a recoverable probe or counter error.
		//
		// Required: false
		Error string `json:"error,omitempty"`

		// ObservedPulls is the number of successful pulls Arcane has initiated for this registry.
		//
		// Required: true
		ObservedPulls int64 `json:"observedPulls"`
	}

	ContainerRegistrySync struct {
		// CreatedAt is the date and time at which the registry was created.
		//
		// Required: true
		CreatedAt time.Time `json:"createdAt"`

		// UpdatedAt is the date and time at which the registry was last updated.
		//
		// Required: true
		UpdatedAt time.Time `json:"updatedAt"`

		// Description of the container registry.
		//
		// Required: false
		Description *string `json:"description,omitzero" unorm:"nfc" trim:"true"`

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

		// RegistryType indicates the type of registry (generic, ecr).
		//
		// Required: true
		RegistryType string `json:"registryType"`

		// AWSAccessKeyID is the AWS Access Key ID for ECR registries.
		// Empty means the agent uses the default AWS credential chain (e.g. its own IAM instance profile).
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

		// RepositoryNames is the list of pre-configured repository namespace
		// strings available for selection when pushing images from the build page.
		//
		// Required: false
		RepositoryNames []string `json:"repositoryNames"`

		// Insecure indicates if the registry uses an insecure connection (HTTP).
		//
		// Required: true
		Insecure bool `json:"insecure"`

		// Enabled indicates if the registry is enabled.
		//
		// Required: true
		Enabled bool `json:"enabled"`
	}

	ContainerRegistryCredential struct {
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

	// ContainerRegistryDigestResult describes a registry digest lookup and the authentication path
	// used to complete it.
	ContainerRegistryDigestResult struct {
		Digest         string
		AuthMethod     string
		AuthUsername   string
		AuthRegistry   string
		UsedCredential bool
	}

	ContainerRegistrySyncRequest struct {
		// Registries is a list of container registries to sync.
		//
		// Required: true
		Registries []ContainerRegistrySync `json:"registries" binding:"required"`
	}
)
