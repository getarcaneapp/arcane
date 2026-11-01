//go:build !buildables

package config

type (
	// BuildablesConfig provides stub fields when buildables are not enabled.
	// This keeps references compile-safe while avoiding env-based configuration.
	BuildablesConfig struct {
		AutoLoginUsername string
		AutoLoginPassword string
	}
)
