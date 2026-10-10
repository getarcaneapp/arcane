package appimages

import (
	"go.getarcane.app/kit/pkg"
	"golang.org/x/mod/semver"
)

// isStableVersion reports whether version is a semver release without a prerelease suffix.
func isStableVersion(version string) bool {
	v := kit.EnsurePrefix(version, "v")
	return semver.IsValid(v) && semver.Prerelease(v) == ""
}
