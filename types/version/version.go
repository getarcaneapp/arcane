package version

type Info struct {
	// CurrentVersion is the current version string.
	//
	// Required: true
	CurrentVersion string `json:"currentVersion"`

	// CurrentTag is the current tag.
	//
	// Required: false
	CurrentTag string `json:"currentTag,omitempty"`

	// CurrentDigest is the current digest (hash) of the version.
	//
	// Required: false
	CurrentDigest string `json:"currentDigest,omitempty"`

	// Revision is the revision identifier (e.g., commit hash).
	//
	// Required: true
	Revision string `json:"revision"`

	// DisplayVersion is the version string formatted for display.
	//
	// Required: true
	DisplayVersion string `json:"displayVersion"`

	// IsSemverVersion indicates if the current version follows semantic versioning.
	//
	// Required: true
	IsSemverVersion bool `json:"isSemverVersion"`

	// NewestVersion is the newest available version string.
	//
	// Required: false
	NewestVersion string `json:"newestVersion,omitempty"`

	// NewestDigest is the digest (hash) of the newest available version.
	//
	// Required: false
	NewestDigest string `json:"newestDigest,omitempty"`

	// UpdateAvailable indicates if an update is available.
	//
	// Required: true
	UpdateAvailable bool `json:"updateAvailable"`

	// ReleaseURL is the URL to the release page.
	//
	// Required: false
	ReleaseURL string `json:"releaseUrl,omitempty"`
}
