//go:build !buildables

package config

// BuildablesConfig provides stub fields when buildables are not enabled.
// This keeps references compile-safe while avoiding env-based configuration.
type BuildablesConfig struct {
	AutoLoginUsername string
	AutoLoginPassword string
}
