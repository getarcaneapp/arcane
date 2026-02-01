// Package projects provides utilities for managing Docker Compose projects and their metadata.
package projects

import (
	"fmt"
	"os"
	"strings"

	"github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/goccy/go-yaml"
)

const (
	// ArcaneIconLabel is the full reverse-DNS label key for service-level icons.
	ArcaneIconLabel = "com.getarcaneapp.arcane.icon"

	arcaneBlockKey     = "x-arcane"
	arcaneIconKey      = "icon"
	arcaneIconsKey     = "icons"
	arcaneURLsKey      = "urls"
	composeServicesKey = "services"
	composeLabelsKey   = "labels"
	composeDeployKey   = "deploy"
)

// ArcaneComposeMetadata represents Arcane-specific configuration extracted from a Compose file.
type ArcaneComposeMetadata struct {
	// ProjectIconURL is the URL to an icon representing the entire project.
	ProjectIconURL string
	// ProjectURLS are additional URLs related to the project (e.g., documentation, homepage).
	ProjectURLS []string
	// ServiceIcons maps service names to their respective icon identifiers or URLs.
	ServiceIcons map[string]string
}

// ParseArcaneComposeMetadata reads a Docker Compose file and extracts Arcane-specific metadata.
func ParseArcaneComposeMetadata(composeFilePath string) (ArcaneComposeMetadata, error) {
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return ArcaneComposeMetadata{}, fmt.Errorf("failed to read compose file: %w", err)
	}

	return ParseArcaneComposeMetadataFromContent(content)
}

// ParseArcaneComposeMetadataFromContent extracts Arcane-specific metadata from a Docker Compose file's content.
func ParseArcaneComposeMetadataFromContent(content []byte) (ArcaneComposeMetadata, error) {
	meta := ArcaneComposeMetadata{ServiceIcons: map[string]string{}}
	if len(content) == 0 {
		return meta, nil
	}

	composeData := map[string]interface{}{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return meta, fmt.Errorf("failed to parse compose content: %w", err)
	}

	if arcaneBlock, ok := utils.AsStringMap(composeData[arcaneBlockKey]); ok {
		meta.ProjectIconURL = utils.FirstNonEmpty(getFirstString(arcaneBlock[arcaneIconKey]), getFirstString(arcaneBlock[arcaneIconsKey]))
		meta.ProjectURLS = utils.UniqueNonEmptyStrings(utils.Collect(arcaneBlock[arcaneURLsKey], utils.ToString))
	}

	services, ok := utils.AsStringMap(composeData[composeServicesKey])
	if !ok {
		return meta, nil
	}

	for name, serviceRaw := range services {
		svc, ok := utils.AsStringMap(serviceRaw)
		if !ok {
			continue
		}

		icon := findArcaneIconLabel(svc[composeLabelsKey])
		if icon == "" {
			if deployBlock, ok := utils.AsStringMap(svc[composeDeployKey]); ok {
				icon = findArcaneIconLabel(deployBlock[composeLabelsKey])
			}
		}

		if icon == "" {
			if arcaneBlock, ok := utils.AsStringMap(svc[arcaneBlockKey]); ok {
				icon = utils.FirstNonEmpty(getFirstString(arcaneBlock[arcaneIconKey]), getFirstString(arcaneBlock[arcaneIconsKey]))
			}
		}

		if icon != "" {
			meta.ServiceIcons[name] = icon
		}
	}

	return meta, nil
}

// getFirstString retrieves the first non-empty string from a value (single or slice).
func getFirstString(v any) string {
	for _, s := range utils.Collect(v, utils.ToString) {
		if s != "" {
			return s
		}
	}
	return ""
}

// findArcaneIconLabel attempts to locate an Arcane icon label within service labels.
// It supports both map[string]string and []string label formats.
func findArcaneIconLabel(labels any) string {
	if labelMap, ok := utils.AsStringMap(labels); ok {
		for key, value := range labelMap {
			if isArcaneIconLabel(key) {
				return utils.ToString(value)
			}
		}
	}

	for _, s := range utils.Collect(labels, utils.ToString) {
		if key, value, ok := parseLabelPair(s); ok && isArcaneIconLabel(key) {
			return value
		}
	}

	return ""
}

// parseLabelPair parses a "KEY=VALUE" string into its components.
func parseLabelPair(raw string) (string, string, bool) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// isArcaneIconLabel checks if a label key matches the Arcane icon label definition.
func isArcaneIconLabel(key string) bool {
	return strings.ToLower(strings.TrimSpace(key)) == ArcaneIconLabel
}
