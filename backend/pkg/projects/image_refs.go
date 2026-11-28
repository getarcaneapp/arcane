package projects

import (
	"cmp"
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	composeapi "github.com/docker/compose/v5/pkg/api"
	mobyImage "github.com/moby/moby/api/types/image"
	mobyClient "github.com/moby/moby/client"

	kit "kit.getarcane.app"
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

func ImageRefsFromComposeServices(services composeTypes.Services) []string {
	serviceNames := slices.Sorted(maps.Keys(services))
	return uniqueImageRefs(len(serviceNames), func(yield func(string)) {
		for _, name := range serviceNames {
			yield(services[name].Image)
		}
	})
}

func BuildImageRefsFromComposeProject(project *composeTypes.Project) []string {
	if project == nil {
		return nil
	}

	serviceNames := slices.Sorted(maps.Keys(project.Services))
	jobs := project.AllJobs()

	return uniqueImageRefs(len(serviceNames)+len(jobs), func(yield func(string)) {
		for _, name := range serviceNames {
			svc := project.Services[name]
			if svc.Build == nil {
				continue
			}
			svc.Name = cmp.Or(svc.Name, name)
			yield(composeapi.GetImageNameOrDefault(svc, project.Name))
		}
		for _, name := range slices.Sorted(maps.Keys(jobs)) {
			if jobs[name].Build == nil {
				continue
			}
			yield(composeapi.GetImageNameOrDefault(composeTypes.ServiceConfig{
				Name:          name,
				ContainerSpec: jobs[name].ContainerSpec,
				WorkloadSpec:  jobs[name].WorkloadSpec,
			}, project.Name))
		}
	})
}

func ImageRefsFrom[T any](services []T, imageRef func(T) string) []string {
	return uniqueImageRefs(len(services), func(yield func(string)) {
		for _, svc := range services {
			yield(imageRef(svc))
		}
	})
}

func uniqueImageRefs(size int, collect func(yield func(string))) []string {
	refs := make([]string, 0, size)
	collect(func(image string) { refs = append(refs, image) })
	return kit.Unique(kit.TrimNonEmpty(refs))
}

func canonicalImageRef(imageRef string) string {
	trimmed := strings.TrimSpace(imageRef)
	if trimmed == "" {
		return ""
	}
	named, err := reference.ParseNormalizedNamed(trimmed)
	if err != nil {
		return trimmed
	}
	return reference.TagNameOnly(named).String()
}

func PullableImageRefs(project *composeTypes.Project) []string {
	return pullableImageRefs(project, false)
}

// DefaultRedeployPullImageRefs returns pullable image references for services
// and jobs that do not declare an explicit pull_policy in Compose.
func DefaultRedeployPullImageRefs(project *composeTypes.Project) []string {
	return pullableImageRefs(project, true)
}

func pullableImageRefs(project *composeTypes.Project, unsetPullPolicyOnly bool) []string {
	if project == nil {
		return nil
	}
	return uniqueImageRefs(len(project.Services), func(yield func(string)) {
		for _, service := range project.Services {
			if unsetPullPolicyOnly && NormalizePullPolicy(service.PullPolicy) != "" {
				continue
			}
			if service.Build == nil {
				yield(service.Image)
			}
			for _, image := range composeapi.GetDependentImages(service, project.Name) {
				yield(image)
			}
			for _, volume := range service.Volumes {
				if volume.Type == composeTypes.VolumeTypeImage {
					yield(volume.Source)
				}
			}
		}
		for _, job := range project.Jobs {
			if unsetPullPolicyOnly && NormalizePullPolicy(job.PullPolicy) != "" {
				continue
			}
			if job.Build == nil {
				yield(job.Image)
			}
			for _, volume := range job.Volumes {
				if volume.Type == composeTypes.VolumeTypeImage {
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

func ComposeImageDigest(inspect mobyClient.ImageInspectResult) string {
	matcher := platforms.Default()
	var available []mobyImage.ManifestSummary
	for _, m := range inspect.Manifests {
		if m.Kind == mobyImage.ManifestKindImage && m.Available {
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
