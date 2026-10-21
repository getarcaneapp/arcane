package types

import (
	"time"
)

// ApiKeyPermissionGrant is one permission granted to an API key, optionally scoped
// to a single environment. Omit EnvironmentID for a global grant.
type ApiKeyPermissionGrant struct {
	Permission    string  `json:"permission" minLength:"1" doc:"Permission string" example:"containers:list"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID to scope the grant to; omit for a global grant"`
}

// ApiKey represents an API key without the secret.
type ApiKey struct {
	ID          string                  `json:"id" readOnly:"true" doc:"Unique identifier of the API key"`
	Name        string                  `json:"name" required:"false" minLength:"1" maxLength:"255" doc:"Name of the API key" example:"My API Key" unorm:"nfc" trim:"true"`
	Description *string                 `json:"description,omitempty" maxLength:"1000" doc:"Description of the API key" unorm:"nfc" trim:"true"`
	KeyPrefix   string                  `json:"keyPrefix" readOnly:"true" doc:"Prefix of the API key for identification"`
	UserID      *string                 `json:"userId,omitempty" readOnly:"true" doc:"ID of the user who owns the API key"`
	Kind        string                  `json:"kind" readOnly:"true" doc:"Key kind: 'scoped' keys use their own permission grants, 'personal' keys inherit the owner's role permissions"`
	IsStatic    bool                    `json:"isStatic" readOnly:"true" doc:"Whether the API key is environment-managed and protected from deletion"`
	IsBootstrap bool                    `json:"isBootstrap" readOnly:"true" doc:"Whether the API key is currently referenced by an environment as its pairing key (locked from manual edit / delete)"`
	ExpiresAt   *time.Time              `json:"expiresAt,omitempty" doc:"Expiration date of the API key"`
	LastUsedAt  *time.Time              `json:"lastUsedAt,omitempty" readOnly:"true" doc:"Last time the API key was used"`
	CreatedAt   time.Time               `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
	UpdatedAt   *time.Time              `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
	Permissions []ApiKeyPermissionGrant `json:"permissions" required:"false" doc:"Permission grants held by this key. Cannot exceed the caller's or owner's own permissions. Ignored for personal keys."`
}

// ApiKeyCreatedDto represents a newly created API key with the full secret.
type ApiKeyCreatedDto struct {
	ApiKey

	Key string `json:"key" readOnly:"true" doc:"The full API key secret (only shown once)"`
}
