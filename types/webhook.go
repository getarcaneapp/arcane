package types

import (
	"time"
)

// Webhook is returned in list responses — the token is masked.
type Webhook struct {
	ID              string     `json:"id" readOnly:"true" doc:"Webhook ID"`
	Name            string     `json:"name" required:"false" minLength:"1" maxLength:"255" doc:"Human-readable name for this webhook" unorm:"nfc" trim:"true"`
	TokenPrefix     string     `json:"tokenPrefix" readOnly:"true" doc:"Masked token prefix for identification"`
	TargetType      string     `json:"targetType" required:"false" doc:"Resource type this webhook targets: 'container', 'project', 'updater', or 'gitops'" enum:"container,project,updater,gitops"`
	ActionType      string     `json:"actionType" required:"false" doc:"Action to run; supported values depend on targetType." enum:"update,start,stop,restart,redeploy,up,down,run,sync"`
	TargetID        string     `json:"targetId" required:"false" doc:"Container ID, project ID, or GitOps sync ID to target. Leave empty for 'updater' webhooks."`
	TargetName      string     `json:"targetName,omitempty" readOnly:"true" doc:"Resolved target resource name when available"`
	EnvironmentID   string     `json:"environmentId" readOnly:"true" doc:"Environment ID"`
	Enabled         bool       `json:"enabled" required:"false" doc:"Whether the webhook is active"`
	LastTriggeredAt *time.Time `json:"lastTriggeredAt,omitempty" readOnly:"true" doc:"Timestamp of last successful trigger"`
	CreatedAt       time.Time  `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
}

// WebhookCreated is returned once when a webhook is first created, including the raw token.
type WebhookCreated struct {
	ID         string    `json:"id" doc:"Webhook ID"`
	Name       string    `json:"name" doc:"Webhook name"`
	Token      string    `json:"token" doc:"Full webhook token — store this securely, it will not be shown again"`
	TargetType string    `json:"targetType" doc:"Target type"`
	ActionType string    `json:"actionType" doc:"Action type"`
	TargetID   string    `json:"targetId" doc:"Target resource ID"`
	CreatedAt  time.Time `json:"createdAt" doc:"Creation timestamp"`
}
