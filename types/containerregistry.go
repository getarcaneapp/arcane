package types

import (
	"time"
)

type (
	ContainerRegistryRepository struct {
		Name string `json:"name"`
	}

	ContainerRegistryRepositoryTag struct {
		Created   *time.Time                     `json:"created,omitempty"`
		Name      string                         `json:"name"`
		Digest    string                         `json:"digest,omitempty"`
		MediaType string                         `json:"mediaType,omitempty"`
		Error     string                         `json:"error,omitempty"`
		Platforms []ContainerRegistryTagPlatform `json:"platforms"`

		Size int64 `json:"size"`
	}

	ContainerRegistryTagPlatform struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant,omitempty"`
		Digest       string `json:"digest"`

		Size int64 `json:"size"`
	}

	ContainerRegistryDeleteTagResponse struct {
		Digest string `json:"digest"`
	}

	ContainerRegistry struct {
		CreatedAt    time.Time `json:"createdAt" readOnly:"true"`
		UpdatedAt    time.Time `json:"updatedAt" readOnly:"true"`
		Description  *string   `json:"description,omitempty" unorm:"nfc" trim:"true"`
		ID           string    `json:"id" readOnly:"true"`
		URL          string    `json:"url" required:"false"`
		Host         string    `json:"host" readOnly:"true"`
		Namespace    string    `json:"namespace" readOnly:"true"`
		Username     string    `json:"username" required:"false"`
		RegistryType string    `json:"registryType" required:"false"`

		AWSAccessKeyID string `json:"awsAccessKeyId,omitempty"`
		AWSRegion      string `json:"awsRegion,omitempty"`

		Token              string   `json:"token,omitempty" writeOnly:"true" copier:"-"`
		AWSSecretAccessKey string   `json:"awsSecretAccessKey,omitempty" writeOnly:"true" copier:"-"`
		RepositoryNames    []string `json:"repositoryNames" required:"false"`
		Insecure           bool     `json:"insecure" required:"false"`
		Enabled            bool     `json:"enabled" required:"false"`
	}

	ContainerRegistryPullUsageResponse struct {
		Registries []ContainerRegistryPullUsage `json:"registries"`
	}

	ContainerRegistryPullUsage struct {
		CheckedAt time.Time `json:"checkedAt"`
		Limit     *int      `json:"limit,omitempty"`
		Remaining *int      `json:"remaining,omitempty"`
		Used      *int      `json:"used,omitempty"`

		WindowSeconds *int   `json:"windowSeconds,omitempty"`
		RegistryID    string `json:"registryId"`
		Provider      string `json:"provider"`
		Registry      string `json:"registry"`
		DisplayName   string `json:"displayName"`
		Repository    string `json:"repository,omitempty"`
		AuthMethod    string `json:"authMethod"`
		AuthUsername  string `json:"authUsername,omitempty"`
		Source        string `json:"source,omitempty"`
		Error         string `json:"error,omitempty"`
		ObservedPulls int64  `json:"observedPulls"`
	}

	ContainerRegistrySync struct {
		CreatedAt    time.Time `json:"createdAt"`
		UpdatedAt    time.Time `json:"updatedAt"`
		Description  *string   `json:"description,omitzero" unorm:"nfc" trim:"true"`
		ID           string    `json:"id" binding:"required"`
		URL          string    `json:"url" binding:"required"`
		Username     string    `json:"username"`
		Token        string    `json:"token"`
		RegistryType string    `json:"registryType"`

		AWSAccessKeyID string `json:"awsAccessKeyId,omitempty"`

		AWSSecretAccessKey string   `json:"awsSecretAccessKey,omitempty"`
		AWSRegion          string   `json:"awsRegion,omitempty"`
		RepositoryNames    []string `json:"repositoryNames"`
		Insecure           bool     `json:"insecure"`
		Enabled            bool     `json:"enabled"`
	}

	ContainerRegistryCredential struct {
		URL      string `json:"url" binding:"required"`
		Username string `json:"username" binding:"required"`
		Token    string `json:"token" binding:"required"`
		Enabled  bool   `json:"enabled"`
	}

	ContainerRegistryDigestResult struct {
		Digest         string
		AuthMethod     string
		AuthUsername   string
		AuthRegistry   string
		UsedCredential bool
	}

	ContainerRegistrySyncRequest struct {
		Registries []ContainerRegistrySync `json:"registries" binding:"required"`
	}
)
