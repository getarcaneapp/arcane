package imageupdate

import (
	"time"

	containerregistry "go.getarcane.app/types/containerregistry"
)

type Response struct {
	// HasUpdate indicates if an update is available for the image.
	//
	// Required: true
	HasUpdate bool `json:"hasUpdate"`

	// UpdateType describes the type of update (e.g., major, minor, patch).
	//
	// Required: true
	UpdateType string `json:"updateType"`

	// CurrentVersion is the current version of the image.
	//
	// Required: true
	CurrentVersion string `json:"currentVersion"`

	// LatestVersion is the latest available version of the image.
	//
	// Required: false
	LatestVersion string `json:"latestVersion,omitempty"`

	// CurrentDigest is the digest (hash) of the current image.
	//
	// Required: false
	CurrentDigest string `json:"currentDigest,omitempty"`

	// LatestDigest is the digest (hash) of the latest available image.
	//
	// Required: false
	LatestDigest string `json:"latestDigest,omitempty"`

	// CheckTime is the time when the update check was performed.
	//
	// Required: true
	CheckTime time.Time `json:"checkTime"`

	// ResponseTimeMs is the response time in milliseconds.
	//
	// Required: true
	ResponseTimeMs int `json:"responseTimeMs"`

	// Error contains any error message from the update check.
	//
	// Required: false
	Error string `json:"error,omitempty"`

	// AuthMethod is the authentication method used ("none" | "anonymous" | "credential" | "unknown").
	//
	// Required: false
	AuthMethod string `json:"authMethod,omitempty"`

	// AuthUsername is the username used for authentication (for credential method).
	//
	// Required: false
	AuthUsername string `json:"authUsername,omitempty"`

	// AuthRegistry is the registry host used for authentication.
	//
	// Required: false
	AuthRegistry string `json:"authRegistry,omitempty"`

	// UsedCredential indicates if credentials were used for the update check.
	//
	// Required: false
	UsedCredential bool `json:"usedCredential,omitempty"`
}

type Summary struct {
	// TotalImages is the total number of images checked.
	//
	// Required: true
	TotalImages int `json:"totalImages"`

	// ImagesWithUpdates is the number of images with available updates.
	//
	// Required: true
	ImagesWithUpdates int `json:"imagesWithUpdates"`

	// DigestUpdates is the number of images with digest updates.
	//
	// Required: true
	DigestUpdates int `json:"digestUpdates"`

	// ErrorsCount is the number of errors encountered during the check.
	//
	// Required: true
	ErrorsCount int `json:"errorsCount"`
}

type BatchImageUpdateRequest struct {
	// ImageRefs is a list of image references to check for updates.
	//
	// Required: true
	ImageRefs []string `json:"imageRefs" binding:"required"`

	// Credentials is a list of container registry credentials for authentication.
	//
	// Required: false
	Credentials []containerregistry.Credential `json:"credentials,omitempty"`
}

type BatchResponse map[string]*Response
