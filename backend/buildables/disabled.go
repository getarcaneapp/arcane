//go:build !buildables

package buildables

var EnabledFeatures = ""

//nolint:shimbad // build-tag stub: the real implementation is compiled under the buildables tag
func HasBuildFeature(feature string) bool {
	return false
}
