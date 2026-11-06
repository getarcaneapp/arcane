package config

var (
	Version          = "dev"
	Revision         = "unknown"
	BuildTime        = "unknown"
	NodeVersion      = "unknown"
	SvelteKitVersion = "unknown"
)

func ShortRevision() string {
	if len(Revision) > 8 {
		return Revision[:8]
	}
	return Revision
}
