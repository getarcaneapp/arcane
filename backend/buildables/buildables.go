//go:build buildables

package buildables

import "strings"

// EnabledFeatures is a comma-separated list of enabled buildable features.
var EnabledFeatures = ""

// HasBuildFeature returns true when the feature is present in EnabledFeatures.
func HasBuildFeature(feature string) bool {
	if feature == "" || EnabledFeatures == "" {
		return false
	}
	for value := range strings.SplitSeq(EnabledFeatures, ",") {
		if strings.EqualFold(strings.TrimSpace(value), feature) {
			return true
		}
	}
	return false
}
