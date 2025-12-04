package config

import "runtime"

var (
	Version   = "dev"
	Revision  = "unknown"
	BuildTime = "unknown"
)

// ShortRevision returns the first 8 characters of the revision hash
func ShortRevision() string {
	if len(Revision) > 8 {
		return Revision[:8]
	}
	return Revision
}

// GoVersion returns the Go runtime version
func GoVersion() string {
	return runtime.Version()
}
