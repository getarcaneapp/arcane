package config

import "runtime"

var (
	Version          = "dev"
	Revision         = "unknown"
	BuildTime        = "unknown"
	NodeVersion      = "unknown"
	SvelteKitVersion = "unknown"
)

const shortRevisionLength = 8

// ShortRevision returns the first 8 characters of the revision hash
func ShortRevision() string {
	revision := Revision
	return revision[:min(len(revision), shortRevisionLength)]
}

// GoVersion returns the Go runtime version
func GoVersion() string {
	return runtime.Version()
}
