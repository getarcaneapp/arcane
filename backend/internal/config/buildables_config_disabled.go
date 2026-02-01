//go:build !buildables

package config

// BuildablesConfig is an empty struct when buildables are not enabled.
// This ensures the main Config struct remains valid but without buildable fields.
type BuildablesConfig struct {
	// No fields when buildables are disabled - this gets pruned away
}
