package appimages

import (
	"golang.org/x/mod/semver"

	"kit.getarcane.app"
)

func isStableVersion(version string) bool {
	v := kit.EnsurePrefix(version, "v")
	return semver.IsValid(v) && semver.Prerelease(v) == ""
}
