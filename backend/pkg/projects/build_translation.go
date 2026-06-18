package projects

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// BuildArgsFromCompose flattens compose build args into a string map, dropping nil values.
func BuildArgsFromCompose(args map[string]*string) map[string]string {
	buildArgs := map[string]string{}
	for key, value := range args {
		if value == nil {
			continue
		}
		buildArgs[key] = *value
	}
	return buildArgs
}

// LabelsFromCompose copies compose labels into a plain map, returning nil when empty.
func LabelsFromCompose(labels composetypes.Labels) map[string]string {
	if len(labels) == 0 {
		return nil
	}

	out := make(map[string]string, len(labels))
	maps.Copy(out, labels)

	return out
}

// UlimitsFromCompose renders compose ulimits as Docker-style "soft:hard" (or single) strings.
func UlimitsFromCompose(ulimits map[string]*composetypes.UlimitsConfig) map[string]string {
	if len(ulimits) == 0 {
		return nil
	}

	out := make(map[string]string, len(ulimits))
	for name, cfg := range ulimits {
		if cfg == nil {
			continue
		}

		switch {
		case cfg.Single > 0:
			out[name] = strconv.Itoa(cfg.Single)
		case cfg.Soft > 0 || cfg.Hard > 0:
			out[name] = fmt.Sprintf("%d:%d", cfg.Soft, cfg.Hard)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// MergeBuildTags combines a primary image tag with compose tags, trimming blanks
// and de-duplicating while preserving order (primary first).
func MergeBuildTags(primaryImage string, composeTags []string) []string {
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(composeTags)+1)

	appendTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		if _, ok := seen[tag]; ok {
			return
		}
		seen[tag] = struct{}{}
		merged = append(merged, tag)
	}

	appendTag(primaryImage)
	for _, tag := range composeTags {
		appendTag(tag)
	}

	return merged
}

// BuildPlatformsFromCompose returns the build platforms for a service, falling
// back to the service platform when no build platforms are declared.
func BuildPlatformsFromCompose(svc composetypes.ServiceConfig) []string {
	platforms := make([]string, 0, len(svc.Build.Platforms)+1)
	for _, platform := range svc.Build.Platforms {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}
		platforms = append(platforms, platform)
	}

	if len(platforms) == 0 {
		if servicePlatform := strings.TrimSpace(svc.Platform); servicePlatform != "" {
			platforms = append(platforms, servicePlatform)
		}
	}

	return platforms
}

// BuildLocalImageTag derives a deterministic local image tag for a built service.
func BuildLocalImageTag(projectID, projectName, serviceName string) string {
	shortID := strings.TrimSpace(projectID)
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	projectPart := SanitizeImageComponent(projectName)
	if projectPart == "" {
		projectPart = "project"
	}
	servicePart := SanitizeImageComponent(serviceName)
	if servicePart == "" {
		servicePart = "service"
	}

	return fmt.Sprintf("arcane.local/%s-%s/%s:latest", projectPart, shortID, servicePart)
}

// SanitizeImageComponent lowercases a value and replaces characters that are
// invalid in an image reference component with '-'.
func SanitizeImageComponent(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, value)
}
