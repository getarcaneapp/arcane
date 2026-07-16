package projects

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
)

// ParseImageRefsJSON parses a JSON array of image references, returning nil
// for empty or invalid input.
func ParseImageRefsJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var refs []string
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil
	}
	return refs
}

// MarshalImageRefsJSON serializes image references to JSON, returning an empty
// string when there are no refs or encoding fails.
func MarshalImageRefsJSON(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	data, err := json.Marshal(refs)
	if err != nil {
		return ""
	}
	return string(data)
}

// ImageRefsFromComposeServices returns unique, non-empty image references from
// a compose service map in stable service-name order.
func ImageRefsFromComposeServices(services composetypes.Services) []string {
	serviceConfigs := make([]composetypes.ServiceConfig, 0, len(services))
	for _, name := range slices.Sorted(maps.Keys(services)) {
		serviceConfigs = append(serviceConfigs, services[name])
	}

	return ImageRefsFromComposeConfigs(serviceConfigs)
}

// ImageRefsFromComposeConfigs returns unique, non-empty image references from
// compose service configs while preserving first-seen order.
func ImageRefsFromComposeConfigs(services []composetypes.ServiceConfig) []string {
	return uniqueImageRefsInternal(services, func(svc composetypes.ServiceConfig) string { return svc.Image })
}

// ImageRefsFromRuntimeServices returns unique, non-empty image references from
// runtime service DTOs while preserving first-seen order.
func ImageRefsFromRuntimeServices(services []projecttypes.RuntimeService) []string {
	return uniqueImageRefsInternal(services, func(svc projecttypes.RuntimeService) string { return svc.Image })
}

func uniqueImageRefsInternal[T any](items []T, image func(T) string) []string {
	refs := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))

	for _, item := range items {
		ref := strings.TrimSpace(image(item))
		if ref == "" {
			continue
		}
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}

	return refs
}
