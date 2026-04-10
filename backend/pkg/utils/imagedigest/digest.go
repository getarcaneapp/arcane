package imagedigest

import (
	"fmt"
	"strings"

	digest "github.com/opencontainers/go-digest"
)

func Normalize(value string) (string, error) {
	parsed, err := digest.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("invalid OCI digest %q: %w", value, err)
	}

	return parsed.String(), nil
}

func FromReferenceSuffix(ref string) (string, bool) {
	_, digestValue, ok := strings.Cut(strings.TrimSpace(ref), "@")
	if !ok {
		return "", false
	}

	normalized, err := Normalize(digestValue)
	if err != nil {
		return "", false
	}

	return normalized, true
}
