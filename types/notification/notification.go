package notification

import (
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/imageupdate"
	"github.com/getarcaneapp/arcane/types/system"
)

// Provider is the type for notification provider identifiers.
type Provider string

const (
	// NotificationProviderDiscord is the builtin Discord notification provider.
	NotificationProviderDiscord Provider = "discord"

	// NotificationProviderEmail is the builtin Email notification provider.
	NotificationProviderEmail Provider = "email"

	// NotificationProviderTelegram is the builtin Telegram notification provider.
	NotificationProviderTelegram Provider = "telegram"

	// NotificationProviderSignal is the builtin Signal notification provider.
	NotificationProviderSignal Provider = "signal"

	// NotificationProviderSlack is the builtin Slack notification provider.
	NotificationProviderSlack Provider = "slack"

	// NotificationProviderNtfy is the builtin Ntfy notification provider.
	NotificationProviderNtfy Provider = "ntfy"

	// NotificationProviderPushover is the builtin Pushover notification provider.
	NotificationProviderPushover Provider = "pushover"

	// NotificationProviderMatrix is the builtin Matrix webhook notification provider.
	NotificationProviderMatrix Provider = "matrix"

	// NotificationProviderGeneric is the builtin Generic webhook notification provider.
	NotificationProviderGeneric Provider = "generic"
)

type Update struct {
	// Provider is the notification provider type.
	//
	// Required: true
	Provider Provider `json:"provider" binding:"required"`

	// Enabled indicates if the notification provider is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// Config contains the provider-specific configuration.
	//
	// Required: true
	Config base.JsonObject `json:"config" binding:"required"`
}

type Response struct {
	// ID is the unique identifier of the notification settings.
	//
	// Required: true
	ID uint `json:"id"`

	// Provider is the notification provider type.
	//
	// Required: true
	Provider Provider `json:"provider"`

	// Enabled indicates if the notification provider is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// Config contains the provider-specific configuration.
	//
	// Required: true
	Config base.JsonObject `json:"config"`
}

type AppriseUpdate struct {
	// APIURL is the URL of the Apprise API endpoint.
	//
	// Required: false
	APIURL string `json:"apiUrl"`

	// Enabled indicates if Apprise is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// ImageUpdateTag is the tag to use for image update notifications.
	//
	// Required: false
	ImageUpdateTag string `json:"imageUpdateTag"`

	// ContainerUpdateTag is the tag to use for container update notifications.
	//
	// Required: false
	ContainerUpdateTag string `json:"containerUpdateTag"`
}

type AppriseResponse struct {
	// ID is the unique identifier of the Apprise settings.
	//
	// Required: true
	ID uint `json:"id"`

	// APIURL is the URL of the Apprise API endpoint.
	//
	// Required: false
	APIURL string `json:"apiUrl"`

	// Enabled indicates if Apprise is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// ImageUpdateTag is the tag to use for image update notifications.
	//
	// Required: false
	ImageUpdateTag string `json:"imageUpdateTag"`

	// ContainerUpdateTag is the tag to use for container update notifications.
	//
	// Required: false
	ContainerUpdateTag string `json:"containerUpdateTag"`
}

type DispatchKind string

const (
	DispatchKindImageUpdate        DispatchKind = "image_update"
	DispatchKindBatchImageUpdate   DispatchKind = "batch_image_update"
	DispatchKindContainerUpdate    DispatchKind = "container_update"
	DispatchKindVulnerabilityFound DispatchKind = "vulnerability_found"
	DispatchKindPruneReport        DispatchKind = "prune_report"
	DispatchKindAutoHeal           DispatchKind = "auto_heal"
)

type DispatchImageUpdate struct {
	ImageRef   string               `json:"imageRef"`
	UpdateInfo imageupdate.Response `json:"updateInfo"`
}

type DispatchBatchImageUpdate struct {
	Updates map[string]*imageupdate.Response `json:"updates"`
}

type DispatchContainerUpdate struct {
	ContainerName string `json:"containerName"`
	ImageRef      string `json:"imageRef"`
	OldDigest     string `json:"oldDigest,omitempty"`
	NewDigest     string `json:"newDigest,omitempty"`
}

type DispatchVulnerabilityFound struct {
	CVEID            string `json:"cveId"`
	CVELink          string `json:"cveLink"`
	Severity         string `json:"severity"`
	ImageName        string `json:"imageName"`
	FixedVersion     string `json:"fixedVersion,omitempty"`
	PkgName          string `json:"pkgName,omitempty"`
	InstalledVersion string `json:"installedVersion,omitempty"`
}

type DispatchPruneReport struct {
	Result system.PruneAllResult `json:"result"`
}

type DispatchAutoHeal struct {
	ContainerName string `json:"containerName"`
	ContainerID   string `json:"containerId"`
}

type DispatchRequest struct {
	Kind               DispatchKind                `json:"kind"`
	ImageUpdate        *DispatchImageUpdate        `json:"imageUpdate,omitempty"`
	BatchImageUpdate   *DispatchBatchImageUpdate   `json:"batchImageUpdate,omitempty"`
	ContainerUpdate    *DispatchContainerUpdate    `json:"containerUpdate,omitempty"`
	VulnerabilityFound *DispatchVulnerabilityFound `json:"vulnerabilityFound,omitempty"`
	PruneReport        *DispatchPruneReport        `json:"pruneReport,omitempty"`
	AutoHeal           *DispatchAutoHeal           `json:"autoHeal,omitempty"`
}
