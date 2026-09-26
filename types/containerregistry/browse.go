package containerregistry

import "time"

// Repository is an image repository stored in a container registry.
type Repository struct {
	// Name of the repository, without the registry host.
	//
	// Required: true
	Name string `json:"name"`
}

// RepositoryTag describes a tag of a repository and the manifest it points to.
type RepositoryTag struct {
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

	// Size is the compressed size in bytes of every platform image.
	//
	// Required: true
	Size int64 `json:"size"`

	// Created is the creation date of the image, when a single platform is published.
	//
	// Required: false
	Created *time.Time `json:"created,omitempty"`

	// Platforms published under the tag.
	//
	// Required: true
	Platforms []TagPlatform `json:"platforms"`

	// Error explains why the manifest details could not be loaded.
	//
	// Required: false
	Error string `json:"error,omitempty"`
}

// TagPlatform describes one platform image published under a tag.
type TagPlatform struct {
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

// DeleteTagResponse reports the manifest removed from the registry.
type DeleteTagResponse struct {
	// Digest of the deleted manifest. Every tag pointing to it is removed.
	//
	// Required: true
	Digest string `json:"digest"`
}
