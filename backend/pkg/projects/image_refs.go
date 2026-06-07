package projects

import (
	"sort"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	projecttypes "github.com/getarcaneapp/arcane/types/project"
)

// ImageRefsFromComposeServices returns unique, non-empty image references from
// a compose service map in stable service-name order.
func ImageRefsFromComposeServices(services composetypes.Services) []string {
	serviceNames := make([]string, 0, len(services))
	for name := range services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	serviceConfigs := make([]composetypes.ServiceConfig, 0, len(services))
	for _, name := range serviceNames {
		serviceConfigs = append(serviceConfigs, services[name])
	}

	return ImageRefsFromComposeConfigs(serviceConfigs)
}

// ImageRefsFromComposeConfigs returns unique, non-empty image references from
// compose service configs while preserving first-seen order.
func ImageRefsFromComposeConfigs(services []composetypes.ServiceConfig) []string {
	return uniqueImageRefsInternal(len(services), func(yield func(string)) {
		for _, svc := range services {
			yield(svc.Image)
		}
	})
}

// ImageRefsFromRuntimeServices returns unique, non-empty image references from
// runtime service DTOs while preserving first-seen order.
func ImageRefsFromRuntimeServices(services []projecttypes.RuntimeService) []string {
	return uniqueImageRefsInternal(len(services), func(yield func(string)) {
		for _, svc := range services {
			yield(svc.Image)
		}
	})
}

func uniqueImageRefsInternal(size int, collect func(yield func(string))) []string {
	refs := make([]string, 0, size)
	seen := make(map[string]struct{}, size)

	collect(func(image string) {
		ref := strings.TrimSpace(image)
		if ref == "" {
			return
		}
		if _, exists := seen[ref]; exists {
			return
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	})

	return refs
}
