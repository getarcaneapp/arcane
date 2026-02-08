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

func normalizeBuildRequest(req imagetypes.BuildRequest, providerName string) imagetypes.BuildRequest {
	if !req.Push && !req.Load {
		if providerName == "depot" {
			req.Push = true
		} else {
			req.Load = true
		}
	}
	return req
}

func validateBuildRequest(req imagetypes.BuildRequest, providerName string) error {
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

func normalizeTags(tags []string) []string {
	seen := map[string]interface{}{}
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
