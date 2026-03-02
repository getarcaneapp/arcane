package libbuild

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
)

func normalizeBuildRequestInternal(req imagetypes.BuildRequest, providerName string) imagetypes.BuildRequest {
	if !req.Push && !req.Load {
		if providerName == "depot" {
			req.Push = true
		} else {
			req.Load = true
		}
	}
	return req
}

func validateBuildRequestInternal(req imagetypes.BuildRequest, providerName string) error {
	if strings.TrimSpace(req.ContextDir) == "" {
		return errors.New("contextDir is required")
	}

	contextDir := filepath.Clean(req.ContextDir)
	if _, err := os.Stat(contextDir); err != nil {
		return fmt.Errorf("build context not found: %w", err)
	}

	if providerName == "depot" && !req.Push {
		return errors.New("depot builds must push images to a registry")
	}

	if unsupported := unsupportedBuildOptionsInternal(req, providerName); len(unsupported) > 0 {
		return fmt.Errorf("unsupported build options for provider %s: %s", providerName, strings.Join(unsupported, ", "))
	}

	if len(req.Tags) == 0 && (req.Push || req.Load) {
		return errors.New("at least one tag is required when push/load is enabled")
	}

	dockerfilePath := strings.TrimSpace(req.Dockerfile)
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}
	fullDockerfilePath := dockerfilePath
	if !filepath.IsAbs(dockerfilePath) {
		fullDockerfilePath = filepath.Join(contextDir, dockerfilePath)
	}
	if _, err := os.Stat(fullDockerfilePath); err != nil {
		return fmt.Errorf("dockerfile not found: %w", err)
	}

	return nil
}

func unsupportedBuildOptionsInternal(req imagetypes.BuildRequest, providerName string) []string {
	unsupported := make([]string, 0, 5)

	switch providerName {
	case "local":
		if hasNonEmptyStringEntriesInternal(req.CacheTo) {
			unsupported = append(unsupported, "cacheTo")
		}
		if hasNonEmptyStringEntriesInternal(req.Entitlements) {
			unsupported = append(unsupported, "entitlements")
		}
		if req.Privileged {
			unsupported = append(unsupported, "privileged")
		}
		if countNonEmptyStringEntriesInternal(req.Platforms) > 1 {
			unsupported = append(unsupported, "platforms")
		}
	case "depot":
		if strings.TrimSpace(req.Network) != "" {
			unsupported = append(unsupported, "network")
		}
		if strings.TrimSpace(req.Isolation) != "" {
			unsupported = append(unsupported, "isolation")
		}
		if req.ShmSize > 0 {
			unsupported = append(unsupported, "shmSize")
		}
		if len(req.Ulimits) > 0 {
			unsupported = append(unsupported, "ulimits")
		}
		if hasNonEmptyStringEntriesInternal(req.ExtraHosts) {
			unsupported = append(unsupported, "extraHosts")
		}
	}

	sort.Strings(unsupported)
	return unsupported
}

func hasNonEmptyStringEntriesInternal(values []string) bool {
	return countNonEmptyStringEntriesInternal(values) > 0
}

func countNonEmptyStringEntriesInternal(values []string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func normalizeTagsInternal(tags []string) []string {
	seen := map[string]any{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = nil
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
