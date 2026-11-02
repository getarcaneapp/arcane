package config

var (
	Version          = "dev"
	Revision         = "unknown"
	BuildTime        = "unknown"
	NodeVersion      = "unknown"
	SvelteKitVersion = "unknown"
)

// ShortRevision returns the first 8 characters of the revision hash
func ShortRevision() string {
	if len(Revision) > 8 {
		return Revision[:8]
	}
	return Revision
}
