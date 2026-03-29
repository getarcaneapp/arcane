package webhook

import "time"

// CreateInput is the request body for creating a webhook.
type CreateInput struct {
	Name       string `json:"name" minLength:"1" maxLength:"255" doc:"Human-readable name for this webhook"`
	TargetType string `json:"targetType" doc:"Type of action to trigger: 'container' updates a single container, 'project' redeploys a compose project, 'updater' runs the environment-wide image updater, 'gitops' triggers a GitOps sync" enum:"container,project,updater,gitops"`
	TargetID   string `json:"targetId" doc:"Container ID, project ID, or GitOps sync ID to target. Leave empty for 'updater' webhooks."`
}

// Summary is returned in list responses — the token is masked.
type Summary struct {
	ID              string     `json:"id" doc:"Webhook ID"`
	Name            string     `json:"name" doc:"Webhook name"`
	TokenPrefix     string     `json:"tokenPrefix" doc:"Masked token prefix for identification"`
	TargetType      string     `json:"targetType" doc:"Target type: 'container', 'project', 'updater', or 'gitops'"`
	TargetID        string     `json:"targetId" doc:"Target resource ID"`
	EnvironmentID   string     `json:"environmentId" doc:"Environment ID"`
	Enabled         bool       `json:"enabled" doc:"Whether the webhook is active"`
	LastTriggeredAt *time.Time `json:"lastTriggeredAt,omitempty" doc:"Timestamp of last successful trigger"`
	CreatedAt       time.Time  `json:"createdAt" doc:"Creation timestamp"`
}

// UpdateInput is the request body for updating a webhook.
type UpdateInput struct {
	Enabled bool `json:"enabled" doc:"Whether the webhook is active"`
}

// Created is returned once when a webhook is first created, including the raw token.
type Created struct {
	ID         string    `json:"id" doc:"Webhook ID"`
	Name       string    `json:"name" doc:"Webhook name"`
	Token      string    `json:"token" doc:"Full webhook token — store this securely, it will not be shown again"`
	TargetType string    `json:"targetType" doc:"Target type"`
	TargetID   string    `json:"targetId" doc:"Target resource ID"`
	CreatedAt  time.Time `json:"createdAt" doc:"Creation timestamp"`
}
