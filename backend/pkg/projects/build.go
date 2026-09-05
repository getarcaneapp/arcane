package projects

import (
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"go.getarcane.app/builds/pkg/contextsource"
	buildtypes "go.getarcane.app/builds/types"
)

// ResolveBuildContext resolves a service build context against workingDir.
// The service config must have a non-nil Build field.
func ResolveBuildContext(workingDir string, svc composetypes.ServiceConfig, serviceName string) (string, error) {
	contextDir := strings.TrimSpace(svc.Build.Context)
	if contextDir == "" {
		contextDir = workingDir
	} else if _, isGitContext, err := contextsource.ParseGitBuildContextSource(contextDir); err != nil {
		return "", errors.WrapIff(err, "invalid build context for service %s", serviceName)
	} else if !isGitContext && !filepath.IsAbs(contextDir) {
		contextDir = filepath.Join(workingDir, contextDir)
	}

	if contextDir == "" {
		return "", errors.Errorf("build context not set for service %s", serviceName)
	}

	return contextDir, nil
}

// ResolveDockerfilePath returns the configured Dockerfile path or Dockerfile.
// The service config must have a non-nil Build field.
func ResolveDockerfilePath(svc composetypes.ServiceConfig) string {
	dockerfilePath := strings.TrimSpace(svc.Build.Dockerfile)
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	return dockerfilePath
}

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

	return fmt.Sprintf("%s/%s-%s/%s:latest", imageref.LocalBuildRegistry, projectPart, shortID, servicePart)
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

func resolveEffectiveBuildProviderInternal(override, defaultProvider string) string {
	provider := strings.ToLower(strings.TrimSpace(override))
	if provider != "" {
		return provider
	}

	provider = strings.ToLower(strings.TrimSpace(defaultProvider))

	if provider == "" {
		provider = "local"
	}

	return provider
}

// PrepareServiceBuildRequest translates a Compose build into the configured provider request.
func PrepareServiceBuildRequest(
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	options projecttypes.BuildOptions,
	defaultProvider string,
) (buildtypes.BuildRequest, composetypes.ServiceConfig, bool, error) {
	if project == nil || svc.Build == nil {
		return buildtypes.BuildRequest{}, svc, false, errors.New("compose project and service build configuration are required")
	}
	imageName, updatedSvc, updated := EnsureServiceImage(projectID, project.Name, serviceName, svc)
	effectiveProvider := resolveEffectiveBuildProviderInternal(options.Provider, defaultProvider)

	if updated && effectiveProvider == "depot" {
		return buildtypes.BuildRequest{}, updatedSvc, updated, errors.Errorf("service %s must define an image when using depot build provider", serviceName)
	}
	if updated && options.Push != nil && *options.Push {
		return buildtypes.BuildRequest{}, updatedSvc, updated, errors.Errorf("service %s must define an image when push is enabled", serviceName)
	}

	// Build providers read the context from Arcane's filesystem, so keep
	// container paths here; only Docker bind sources use host paths.
	contextDir, err := ResolveBuildContext(project.WorkingDir, updatedSvc, serviceName)
	if err != nil {
		return buildtypes.BuildRequest{}, updatedSvc, updated, err
	}

	dockerfileInline := updatedSvc.Build.DockerfileInline
	if strings.TrimSpace(updatedSvc.Build.Dockerfile) != "" && strings.TrimSpace(dockerfileInline) != "" {
		return buildtypes.BuildRequest{}, updatedSvc, updated, errors.Errorf("service %s cannot define both dockerfile and dockerfile_inline", serviceName)
	}

	dockerfilePath := ""
	if strings.TrimSpace(dockerfileInline) == "" {
		dockerfilePath = ResolveDockerfilePath(updatedSvc)
	}

	buildReq := buildtypes.BuildRequest{
		ContextDir:       contextDir,
		Dockerfile:       dockerfilePath,
		DockerfileInline: dockerfileInline,
		Tags:             MergeBuildTags(imageName, updatedSvc.Build.Tags),
		Target:           strings.TrimSpace(updatedSvc.Build.Target),
		BuildArgs:        BuildArgsFromCompose(updatedSvc.Build.Args),
		Labels:           LabelsFromCompose(updatedSvc.Build.Labels),
		CacheFrom:        append([]string(nil), updatedSvc.Build.CacheFrom...),
		CacheTo:          append([]string(nil), updatedSvc.Build.CacheTo...),
		NoCache:          updatedSvc.Build.NoCache,
		Pull:             updatedSvc.Build.Pull,
		Network:          strings.TrimSpace(updatedSvc.Build.Network),
		Isolation:        strings.TrimSpace(updatedSvc.Build.Isolation),
		ShmSize:          int64(updatedSvc.Build.ShmSize),
		Ulimits:          UlimitsFromCompose(updatedSvc.Build.Ulimits),
		Entitlements: append(
			[]string(nil),
			updatedSvc.Build.Entitlements...,
		),
		Privileged: updatedSvc.Build.Privileged,
		ExtraHosts: updatedSvc.Build.ExtraHosts.AsList(":"),
		Platforms:  BuildPlatformsFromCompose(updatedSvc),
		Provider:   effectiveProvider,
	}
	if options.Push != nil {
		buildReq.Push = *options.Push
	}
	if options.Load != nil {
		buildReq.Load = *options.Load
	}

	return buildReq, updatedSvc, updated, nil
}
