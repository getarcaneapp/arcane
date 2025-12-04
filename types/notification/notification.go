package notification

import "go.getarcane.app/types/base"

// Provider is the type for notification provider identifiers.
type Provider string

const (
	// NotificationProviderDiscord is the builtin Discord notification provider.
	NotificationProviderDiscord Provider = "discord"

	// NotificationProviderEmail is the builtin Email notification provider.
	NotificationProviderEmail Provider = "email"
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
