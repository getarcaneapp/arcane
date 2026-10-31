package types

import (
	"time"
)

type ApnsDevice struct {
	CreatedAt      time.Time       `json:"createdAt" readOnly:"true"`
	Events         map[string]bool `json:"events" required:"false"`
	LastSeenAt     *time.Time      `json:"lastSeenAt,omitempty" readOnly:"true"`
	ID             string          `json:"id" readOnly:"true"`
	Label          string          `json:"label" required:"false" maxLength:"100" unorm:"nfc" trim:"true"`
	EnvironmentIDs []string        `json:"environmentIds" required:"false"`
}

type ApnsStatus struct {
	ChannelID string       `json:"channelId,omitempty"`
	RelayURL  string       `json:"relayUrl"`
	Devices   []ApnsDevice `json:"devices"`
	Enabled   bool         `json:"enabled"`
}

type ApnsPairingToken struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Token     string    `json:"token"`
	ChannelID string    `json:"channelId"`
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
	OccurredAt      time.Time     `json:"occurredAt"`
	Resource        *ApnsResource `json:"resource,omitempty"`
	Route           ApnsRoute     `json:"route"`
	EventID         string        `json:"eventId"`
	Type            string        `json:"type"`
	Severity        string        `json:"severity"`
	EnvironmentID   string        `json:"environmentId"`
	EnvironmentName string        `json:"environmentName"`
	Title           string        `json:"title"`
	Body            string        `json:"body"`
	ChannelID       string        `json:"channelId"`
	RecipientIDs    []string      `json:"recipientIds"`
	Version         int           `json:"v"`
}
