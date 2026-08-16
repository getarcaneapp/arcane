package projects

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
)

const (
	// ProjectTagMaxLength is the maximum number of Unicode characters in a tag name.
	ProjectTagMaxLength = 64
	// ProjectTagsPerSourceLimit is the maximum number of UI or Compose tags on one project.
	ProjectTagsPerSourceLimit = 50
)

var projectTagColorsInternal = map[projecttypes.TagColor]struct{}{
	projecttypes.TagColorGray:   {},
	projecttypes.TagColorPurple: {},
	projecttypes.TagColorBlue:   {},
	projecttypes.TagColorGreen:  {},
	projecttypes.TagColorYellow: {},
	projecttypes.TagColorOrange: {},
	projecttypes.TagColorRed:    {},
	projecttypes.TagColorPink:   {},
}

// NormalizeProjectTag validates and normalizes a project tag name.
func NormalizeProjectTag(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", errors.New("tag name cannot be empty")
	}
	if len([]rune(normalized)) > ProjectTagMaxLength {
		return "", fmt.Errorf("tag name cannot exceed %d characters", ProjectTagMaxLength)
	}
	if strings.ContainsRune(normalized, ',') {
		return "", errors.New("tag name cannot contain commas")
	}
	for _, char := range normalized {
		if unicode.IsControl(char) {
			return "", errors.New("tag name cannot contain control characters")
		}
	}
	return normalized, nil
}

// NormalizeProjectTags validates, normalizes, and de-duplicates project tags.
func NormalizeProjectTags(names []string) ([]string, error) {
	result := make([]string, 0, min(len(names), ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		normalized, err := NormalizeProjectTag(name)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		if len(result) >= ProjectTagsPerSourceLimit {
			return nil, fmt.Errorf("a project cannot have more than %d tags per source", ProjectTagsPerSourceLimit)
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

// NormalizeProjectTagColor validates and normalizes a project tag color.
func NormalizeProjectTagColor(color projecttypes.TagColor) (projecttypes.TagColor, error) {
	normalized := projecttypes.TagColor(strings.ToLower(strings.TrimSpace(string(color))))
	if normalized == "" {
		return projecttypes.TagColorGray, nil
	}
	if _, valid := projectTagColorsInternal[normalized]; !valid {
		return "", fmt.Errorf("unsupported tag color %q", color)
	}
	return normalized, nil
}
