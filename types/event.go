package types

import (
	"time"
)

type (
	EventSeverityCounts struct {
		Total   int64 `json:"total"`
		Info    int64 `json:"info"`
		Success int64 `json:"success"`
		Warning int64 `json:"warning"`
		Error   int64 `json:"error"`
	}

	Event struct {
		Timestamp     time.Time      `json:"timestamp"`
		CreatedAt     time.Time      `json:"createdAt"`
		ResourceType  *string        `json:"resourceType,omitempty"`
		ResourceID    *string        `json:"resourceId,omitempty"`
		ResourceName  *string        `json:"resourceName,omitempty"`
		UserID        *string        `json:"userId,omitempty"`
		Username      *string        `json:"username,omitempty"`
		EnvironmentID *string        `json:"environmentId,omitempty"`
		Metadata      map[string]any `json:"metadata,omitempty"`
		UpdatedAt     *time.Time     `json:"updatedAt,omitempty"`
		ID            string         `json:"id"`
		Type          string         `json:"type"`
		Severity      string         `json:"severity"`
		Title         string         `json:"title"`
		Description   string         `json:"description,omitempty"`
	}

	EventStreamEvent struct {
		Type      string               `json:"type"`
		Timestamp time.Time            `json:"timestamp"`
		Changes   []EnvironmentChanges `json:"changes"`
	}

	EnvironmentChanges struct {
		Kinds         map[string]uint64 `json:"kinds"`
		EnvironmentID string            `json:"environmentId"`
		Epoch         string            `json:"epoch"`
	}
)
