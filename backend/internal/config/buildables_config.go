//go:build buildables

package config

// BuildablesConfig contains configuration for features that can be conditionally compiled.
// These fields are only available when the app is built with the buildables tag.
type BuildablesConfig struct {
	// Auto-login credentials (used only when built with the buildables tag + autologin feature flag).
	AutoLoginUsername string `env:"AUTO_LOGIN_USERNAME" default:"arcane"`
	AutoLoginPassword string `env:"AUTO_LOGIN_PASSWORD" default:"arcane-admin" options:"file"`
}
