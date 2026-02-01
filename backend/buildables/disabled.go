//go:build !buildables

package buildables

// EnabledFeatures is empty when the buildables tag is not set.
var EnabledFeatures = ""

// HasBuildFeature always returns false when the buildables tag is not set.
func HasBuildFeature(feature string) bool {
	return false
}
