package projects

import (
	"cmp"
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/platforms"
	composeapi "github.com/docker/compose/v5/pkg/api"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	kit "go.getarcane.app/kit/pkg"
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
	return kit.Ternary(err != nil, "", string(data))
}

// ImageRefsFromComposeServices returns unique, non-empty image references from
// a compose service map in stable service-name order.
func ImageRefsFromComposeServices(services composetypes.Services) []string {
	serviceNames := slices.Sorted(maps.Keys(services))

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

	serviceNames := slices.Sorted(maps.Keys(project.Services))

	return uniqueImageRefsInternal(len(serviceNames), func(yield func(string)) {
		for _, name := range serviceNames {
			svc := project.Services[name]
			if svc.Build == nil {
				continue
			}
			svc.Name = cmp.Or(svc.Name, name)
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
	collect(func(image string) { refs = append(refs, image) })
	return kit.Unique(kit.TrimNonEmpty(refs))
}

// PullableImageRefs includes service images and registry-only hook and volume images.
func PullableImageRefs(project *composetypes.Project) []string {
	if project == nil {
		return nil
	}
	return uniqueImageRefsInternal(len(project.Services), func(yield func(string)) {
		for _, service := range project.Services {
			if service.Build == nil {
				yield(service.Image)
			}
			for _, image := range composeapi.GetDependentImages(service, project.Name) {
				yield(image)
			}
			for _, volume := range service.Volumes {
				if volume.Type == composetypes.VolumeTypeImage {
					yield(volume.Source)
				}
			}
		}
	})
}

// RefreshComposeImageLabel updates an existing Compose digest label in place.
func RefreshComposeImageLabel(labels map[string]string, imageDigest string) {
	if imageDigest == "" {
		return
	}
	for key := range labels {
		if strings.EqualFold(key, composeapi.ImageDigestLabel) {
			labels[key] = imageDigest
		}
	}
}

// ComposeImageDigest selects the digest compose v5.5.0 records in the
// com.docker.compose.image label: the host platform's image-manifest digest,
// which stays stable across attested rebuilds, falling back to the top-level
// image ID when the engine reports no manifest data (mirrors compose's
// localContentDigest). Writing any other value would make compose flag the
// container as diverged and recreate it on the next up.
func ComposeImageDigest(inspect client.ImageInspectResult) string {
	matcher := platforms.Default()
	var available []image.ManifestSummary
	for _, m := range inspect.Manifests {
		if m.Kind == image.ManifestKindImage && m.Available {
			available = append(available, m)
		}
	}
	for _, m := range available {
		if m.ImageData != nil && matcher.Match(m.ImageData.Platform) {
			return m.ID
		}
	}
	if len(available) == 1 {
		return available[0].ID
	}
	return inspect.ID
}
