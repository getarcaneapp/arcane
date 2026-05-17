package docker

import "strings"

// ReplaceImageTag swaps or adds a tag on an image reference and drops any digest.
func ReplaceImageTag(imageRef string, tag string) string {
	imageRef = strings.TrimSpace(imageRef)
	tag = strings.TrimSpace(tag)
	if imageRef == "" || tag == "" {
		return imageRef
	}
	if beforeDigest, _, found := strings.Cut(imageRef, "@"); found {
		imageRef = beforeDigest
	}

	slashIndex := strings.LastIndex(imageRef, "/")
	colonIndex := strings.LastIndex(imageRef, ":")
	if colonIndex > slashIndex {
		return imageRef[:colonIndex+1] + tag
	}
	return imageRef + ":" + tag
}
