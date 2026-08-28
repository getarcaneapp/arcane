// Package rustic provides shared Rustic runtime configuration.
package rustic

// DefaultImage is the official Rustic container image used by backup and recovery flows.
const DefaultImage = "ghcr.io/rustic-rs/rustic:v0.11.2"

// CacheVolume persists Rustic's repository cache across ephemeral runtime containers.
const CacheVolume = "arcane-rustic-cache"
