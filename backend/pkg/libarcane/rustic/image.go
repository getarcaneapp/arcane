// Package rustic provides shared Rustic runtime configuration.
package rustic

import "runtime"

const (
	// DefaultImage is the official Rustic container image used by backup and recovery flows.
	DefaultImage = "ghcr.io/rustic-rs/rustic:v0.11.2"

	// WindowsImage packages the official Rustic Windows binary for Windows Docker hosts,
	// where DefaultImage has no variant.
	WindowsImage = "ghcr.io/phb-fra/rustic-windows:v0.11.2"

	// CacheVolume persists Rustic's repository cache across ephemeral runtime containers.
	CacheVolume = "arcane-rustic-cache"
)

// Image returns the Rustic image for the Docker host Arcane runs on. A Windows
// Arcane binary runs in a Windows container, so its helpers are Windows containers too.
func Image() string {
	if runtime.GOOS == "windows" {
		return WindowsImage
	}
	return DefaultImage
}
