package appimages

import (
	"golang.org/x/mod/semver"
	"kit.getarcane.app"
)

// isStableVersion reports whether version is a semver release without a prerelease suffix.
func isStableVersion(version string) bool {
	v := kit.EnsurePrefix(version, "v")
	return semver.IsValid(v) && semver.Prerelease(v) == ""
}
