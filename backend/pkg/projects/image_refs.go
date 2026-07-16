package projects

import (
	"encoding/json"
	"sort"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	composeapi "github.com/docker/compose/v5/pkg/api"
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

// BuildImageRefsFromComposeProject returns the image references produced by
// services with build directives, including Compose's default image name when
// a service does not declare image explicitly.
func BuildImageRefsFromComposeProject(project *composetypes.Project) []string {
	if project == nil {
		return nil
	}

	serviceNames := make([]string, 0, len(project.Services))
	for name := range project.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	return uniqueImageRefsInternal(len(serviceNames), func(yield func(string)) {
		for _, name := range serviceNames {
			svc := project.Services[name]
			if svc.Build == nil {
				continue
			}
			if svc.Name == "" {
				svc.Name = name
			}
			yield(composeapi.GetImageNameOrDefault(svc, project.Name))
		}
	})
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
