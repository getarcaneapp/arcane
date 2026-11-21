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
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
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

func projectBuiltImageKeys(project *composeTypes.Project) map[string]struct{} {
	if project == nil {
		return nil
	}
	built := map[string]struct{}{}
	add := func(img string) {
		if key := canonicalImageRef(img); key != "" {
			built[key] = struct{}{}
		}
	}
	for _, svc := range project.Services {
		if svc.Build == nil {
			continue
		}
		add(svc.Image)
		for _, tag := range svc.Build.Tags {
			add(tag)
		}
	}
	for _, job := range project.Jobs {
		if job.Build == nil {
			continue
		}
		add(job.Image)
		for _, tag := range job.Build.Tags {
			add(tag)
		}
	}
	return built
}

func PullableImageRefs(project *composeTypes.Project) []string {
	if project == nil {
		return nil
	}
	built := projectBuiltImageKeys(project)
	return uniqueImageRefs(len(project.Services), func(yield func(string)) {
		yieldIfPullable := func(img string) {
			if _, isBuilt := built[canonicalImageRef(img)]; !isBuilt {
				yield(img)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(project.Services)) {
			service := project.Services[name]
			if service.Build == nil {
				yieldIfPullable(service.Image)
			}
			for _, img := range composeapi.GetDependentImages(service, project.Name) {
				yieldIfPullable(img)
			}
			for _, volume := range service.Volumes {
				if volume.Type == composeTypes.VolumeTypeImage {
					yieldIfPullable(volume.Source)
				}
			}
		}
		for _, name := range slices.Sorted(maps.Keys(project.Jobs)) {
			job := project.Jobs[name]
			if job.Build == nil {
				yieldIfPullable(job.Image)
			}
			for _, volume := range job.Volumes {
				if volume.Type == composeTypes.VolumeTypeImage {
					yieldIfPullable(volume.Source)
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
