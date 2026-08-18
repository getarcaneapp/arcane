package projects

import (
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// NormalizeBuildSelections turns a user-supplied service list into a lookup set,
// dropping blanks. An empty set means "every service".
func NormalizeBuildSelections(services []string) map[string]struct{} {
	selected := map[string]struct{}{}
	for _, name := range services {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		selected[name] = struct{}{}
	}
	return selected
}

// ServiceSelected reports whether name is in the selection built by
// NormalizeBuildSelections. An empty selection selects everything.
func ServiceSelected(selected map[string]struct{}, name string) bool {
	if len(selected) == 0 {
		return true
	}
	_, ok := selected[name]
	return ok
}

// EnsureServiceImage gives a build-only service a deterministic local image tag
// so the built image has a stable name to deploy from. It reports whether the
// service config was changed.
func EnsureServiceImage(projectID, projectName, serviceName string, svc composetypes.ServiceConfig) (string, composetypes.ServiceConfig, bool) {
	imageName := strings.TrimSpace(svc.Image)
	if imageName == "" {
		imageName = BuildLocalImageTag(projectID, projectName, serviceName)
		svc.Image = imageName
		return imageName, svc, true
	}
	return imageName, svc, false
}

// PrepareDeployServiceConfig resolves the image a service deploys as, naming
// build-only services via EnsureServiceImage. It reports whether the service
// config was changed.
func PrepareDeployServiceConfig(projectID, projectName, serviceName string, svc composetypes.ServiceConfig) (composetypes.ServiceConfig, string, bool) {
	if svc.Build == nil {
		return svc, strings.TrimSpace(svc.Image), false
	}

	resolvedImage, updatedSvc, updated := EnsureServiceImage(projectID, projectName, serviceName, svc)
	return updatedSvc, resolvedImage, updated
}

// ShouldPullDeployImage reports whether a deploy must pull, given the resolved
// pull decision, whether the image is already present locally, and when the
// local image was last tagged (zero when unknown). Refresh policies mirror
// compose v5.5.0's `up`: a present image is re-pulled only once its window
// has elapsed since the engine's last-tag time.
func ShouldPullDeployImage(decision DeployImageDecision, exists bool, lastTagged time.Time) bool {
	if decision.PullAlways {
		return true
	}
	if !exists {
		return decision.PullIfMissing || decision.PullIfStale
	}
	if decision.PullIfStale {
		return lastTagged.IsZero() || time.Now().After(lastTagged.Add(decision.StaleAfter))
	}
	return false
}

// SelectedImageRefs returns the distinct pullable image refs of the selected
// services — build services are excluded, since their images come from a build.
func SelectedImageRefs(compProj *composetypes.Project, servicesToUpdate []string) []string {
	if compProj == nil {
		return nil
	}

	selected := NormalizeBuildSelections(servicesToUpdate)
	refs := make([]string, 0, len(compProj.Services))
	seen := make(map[string]struct{}, len(compProj.Services))

	for name, svc := range compProj.Services {
		if !ServiceSelected(selected, name) || svc.Build != nil {
			continue
		}

		imageRef := strings.TrimSpace(svc.Image)
		if imageRef == "" {
			continue
		}
		if _, exists := seen[imageRef]; exists {
			continue
		}

		seen[imageRef] = struct{}{}
		refs = append(refs, imageRef)
	}

	return refs
}
