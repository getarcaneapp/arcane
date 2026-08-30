package apns

import "time"

type Device struct {
	ID             string          `json:"id"`
	Label          string          `json:"label"`
	Events         map[string]bool `json:"events"`
	EnvironmentIDs []string        `json:"environmentIds"`
	CreatedAt      time.Time       `json:"createdAt"`
	LastSeenAt     *time.Time      `json:"lastSeenAt,omitempty"`
}

type Status struct {
	Enabled   bool     `json:"enabled"`
	ChannelID string   `json:"channelId,omitempty"`
	RelayURL  string   `json:"relayUrl"`
	Devices   []Device `json:"devices"`
}

type PairingToken struct {
	Token     string    `json:"token"`
	ChannelID string    `json:"channelId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type RegisterDeviceRequest struct {
	RecipientID    string          `json:"recipientId" minLength:"1" maxLength:"128"`
	Label          string          `json:"label" maxLength:"100"`
	Events         map[string]bool `json:"events,omitzero"`
	EnvironmentIDs []string        `json:"environmentIds,omitzero"`
}

type UpdateDeviceRequest struct {
	Label          *string          `json:"label,omitzero" maxLength:"100"`
	Events         *map[string]bool `json:"events,omitzero"`
	EnvironmentIDs *[]string        `json:"environmentIds,omitzero"`
}

type Resource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type Route struct {
	Kind          string `json:"kind"`
	Tab           string `json:"tab,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
	ID            string `json:"id,omitempty"`
}

type Envelope struct {
	Version         int       `json:"v"`
	EventID         string    `json:"eventId"`
	OccurredAt      time.Time `json:"occurredAt"`
	Type            string    `json:"type"`
	Severity        string    `json:"severity"`
	EnvironmentID   string    `json:"environmentId"`
	EnvironmentName string    `json:"environmentName"`
	Resource        *Resource `json:"resource,omitempty"`
	Title           string    `json:"title"`
	Body            string    `json:"body"`
	Route           Route     `json:"route"`
	ChannelID       string    `json:"channelId"`
	RecipientIDs    []string  `json:"recipientIds"`
}
