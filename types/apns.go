package types

import (
	"time"
)

type ApnsDevice struct {
	ID             string          `json:"id" readOnly:"true"`
	Label          string          `json:"label" required:"false" maxLength:"100" unorm:"nfc" trim:"true"`
	Events         map[string]bool `json:"events" required:"false"`
	EnvironmentIDs []string        `json:"environmentIds" required:"false"`
	CreatedAt      time.Time       `json:"createdAt" readOnly:"true"`
	LastSeenAt     *time.Time      `json:"lastSeenAt,omitempty" readOnly:"true"`
}

type ApnsStatus struct {
	Enabled   bool         `json:"enabled"`
	ChannelID string       `json:"channelId,omitempty"`
	RelayURL  string       `json:"relayUrl"`
	Devices   []ApnsDevice `json:"devices"`
}

type ApnsPairingToken struct {
	Token     string    `json:"token"`
	ChannelID string    `json:"channelId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ApnsRegisterDeviceRequest struct {
	RecipientID    string          `json:"recipientId" minLength:"1" maxLength:"128"`
	Label          string          `json:"label" maxLength:"100" unorm:"nfc" trim:"true"`
	Events         map[string]bool `json:"events,omitzero"`
	EnvironmentIDs []string        `json:"environmentIds,omitzero"`
}

type ApnsResource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type ApnsRoute struct {
	Kind          string `json:"kind"`
	Tab           string `json:"tab,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
	ID            string `json:"id,omitempty"`
}

type ApnsEnvelope struct {
	Version         int           `json:"v"`
	EventID         string        `json:"eventId"`
	OccurredAt      time.Time     `json:"occurredAt"`
	Type            string        `json:"type"`
	Severity        string        `json:"severity"`
	EnvironmentID   string        `json:"environmentId"`
	EnvironmentName string        `json:"environmentName"`
	Resource        *ApnsResource `json:"resource,omitempty"`
	Title           string        `json:"title"`
	Body            string        `json:"body"`
	Route           ApnsRoute     `json:"route"`
	ChannelID       string        `json:"channelId"`
	RecipientIDs    []string      `json:"recipientIds"`
}
