package secretsource

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	// SecretSource is a saved connection to an external secret provider.
	// Credential holds ciphertext; Settings never holds secrets.
	SecretSource struct {
		Settings      types.SecretSourceSettings `gorm:"column:settings;serializer:json"`
		LastTestedAt  *time.Time                 `gorm:"column:last_tested_at"`
		LastTestError *string                    `gorm:"column:last_test_error"`
		database.BaseModel
		Name       string `gorm:"column:name"`
		Provider   string `gorm:"column:provider"`
		Credential string `gorm:"column:credential"`
	}

	// ProjectSecretBinding connects a project to one provider target; a project
	// can have several, applied in Position order. It stores no secret values:
	// the hashes are salted HMACs of one binding's secret set (what the last
	// deploy used, what the last check saw, and the last change already announced,
	// so a change is reported and auto-redeployed only once), and DeployedKeys
	// are the names the last deploy delivered.
	//
	// The table is secret_bindings, with an owner kind, so standalone containers
	// can be bound later without another table; only projects are supported now.
	ProjectSecretBinding struct {
		Source           *SecretSource                   `gorm:"foreignKey:SourceID"`
		Target           types.SecretSourceBindingTarget `gorm:"column:target;serializer:json"`
		DeployedHash     *string                         `gorm:"column:deployed_hash"`
		DeployedAt       *time.Time                      `gorm:"column:deployed_at"`
		LastSeenHash     *string                         `gorm:"column:last_seen_hash"`
		LastNotifiedHash *string                         `gorm:"column:last_notified_hash"`
		LastCheckedAt    *time.Time                      `gorm:"column:last_checked_at"`
		LastFetchedAt    *time.Time                      `gorm:"column:last_fetched_at"`
		LastFetchError   *string                         `gorm:"column:last_fetch_error"`
		database.BaseModel
		OwnerKind    string   `gorm:"column:owner_kind"`
		ProjectID    string   `gorm:"column:project_id"`
		SourceID     string   `gorm:"column:source_id"`
		HashSalt     string   `gorm:"column:hash_salt"`
		DeployedKeys []string `gorm:"column:deployed_keys;serializer:json"`
		Position     int      `gorm:"column:position"`
		Required     bool     `gorm:"column:required"`
		Enabled      bool     `gorm:"column:enabled"`
		AutoRedeploy bool     `gorm:"column:auto_redeploy"`
	}
)

func (SecretSource) TableName() string { return "secret_sources" }

func (ProjectSecretBinding) TableName() string { return "secret_bindings" }

// redeployNeededInternal reports whether the last check saw a different secret
// set than the last deploy used.
func (b *ProjectSecretBinding) redeployNeededInternal() bool {
	return b.DeployedHash != nil && b.LastSeenHash != nil && *b.LastSeenHash != *b.DeployedHash
}
