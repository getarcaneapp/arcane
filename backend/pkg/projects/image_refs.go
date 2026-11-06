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
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"

	"kit.getarcane.app"

	"getarcane.dev/types/v3"
)

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

func MarshalImageRefsJSON(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	data, err := json.Marshal(refs)
	return kit.Ternary(err != nil, "", string(data))
}

func ImageRefsFromComposeServices(services composetypes.Services) []string {
	serviceNames := slices.Sorted(maps.Keys(services))

	serviceConfigs := make([]composetypes.ServiceConfig, 0, len(services))
	for _, name := range serviceNames {
		serviceConfigs = append(serviceConfigs, services[name])
	}

	return ImageRefsFromComposeConfigs(serviceConfigs)
}

func BuildImageRefsFromComposeProject(project *composetypes.Project) []string {
	if project == nil {
		return nil
	}

	serviceNames := slices.Sorted(maps.Keys(project.Services))

	return uniqueImageRefs(len(serviceNames), func(yield func(string)) {
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

func ImageRefsFromComposeConfigs(services []composetypes.ServiceConfig) []string {
	return uniqueImageRefs(len(services), func(yield func(string)) {
		for _, svc := range services {
			yield(svc.Image)
		}
	})
}

func ImageRefsFromRuntimeServices(services []types.ProjectRuntimeService) []string {
	return uniqueImageRefs(len(services), func(yield func(string)) {
		for _, svc := range services {
			yield(svc.Image)
		}
	})
}

func uniqueImageRefs(size int, collect func(yield func(string))) []string {
	refs := make([]string, 0, size)
	collect(func(image string) { refs = append(refs, image) })
	return kit.Unique(kit.TrimNonEmpty(refs))
}

func PullableImageRefs(project *composetypes.Project) []string {
	if project == nil {
		return nil
	}
	return uniqueImageRefs(len(project.Services), func(yield func(string)) {
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
