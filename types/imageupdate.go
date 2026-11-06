package types

import (
	"time"
)

type (
	ImageUpdateResponse struct {
		CheckTime        time.Time                       `json:"checkTime"`
		ImageUpdate      *ImageUpdateResponse            `json:"imageUpdate,omitempty"`
		ContainerUpdates map[string]*ImageUpdateResponse `json:"containerUpdates,omitempty"`
		ActivityID       *string                         `json:"activityId,omitempty"`
		ImageRef         string                          `json:"imageRef,omitempty"`
		UpdateType       string                          `json:"updateType"`
		CurrentVersion   string                          `json:"currentVersion"`
		LatestVersion    string                          `json:"latestVersion,omitempty"`
		CurrentDigest    string                          `json:"currentDigest,omitempty"`
		LatestDigest     string                          `json:"latestDigest,omitempty"`

		CurrentCreated *time.Time `json:"currentCreated,omitempty"`

		LatestCreated *time.Time `json:"latestCreated,omitempty"`
		Error         string     `json:"error,omitempty"`
		AuthMethod    string     `json:"authMethod,omitempty"`
		AuthUsername  string     `json:"authUsername,omitempty"`
		AuthRegistry  string     `json:"authRegistry,omitempty"`
		ReleaseURL    string     `json:"releaseUrl,omitempty"`

		ResponseTimeMs int  `json:"responseTimeMs"`
		HasUpdate      bool `json:"hasUpdate"`
		UsedCredential bool `json:"usedCredential,omitempty"`
	}

	ImageUpdateSummary struct {
		TotalImages       int `json:"totalImages"`
		ImagesWithUpdates int `json:"imagesWithUpdates"`
		DigestUpdates     int `json:"digestUpdates"`
		ErrorsCount       int `json:"errorsCount"`
	}

	BatchImageUpdateRequest struct {
		ImageRefs   []string                      `json:"imageRefs,omitempty"`
		Credentials []ContainerRegistryCredential `json:"credentials,omitempty"`
	}

	ImageUpdateCheckRequest struct {
		ImageRefs []string `json:"imageRefs,omitempty"`
		All       bool     `json:"all,omitempty"`
	}

	ImageUpdateBatchResponse map[string]*ImageUpdateResponse
)
