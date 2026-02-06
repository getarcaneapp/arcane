// Package projects provides utilities for managing Docker Compose projects and their metadata.
package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/goccy/go-yaml"
)

const (
	// ArcaneIconLabel is the full reverse-DNS label key for service-level icons.
	ArcaneIconLabel = "com.getarcaneapp.arcane.icon"

	arcaneBlockKey = "x-arcane"
	arcaneIconKey  = "icon"
	arcaneIconsKey = "icons"
	arcaneURLsKey  = "urls"
)

// ArcaneComposeMetadata represents Arcane-specific configuration extracted from a Compose file.
type ArcaneComposeMetadata struct {
	// ProjectIconURL is the URL to an icon representing the entire project.
	ProjectIconURL string
	// ProjectURLS are additional URLs related to the project (e.g., documentation, homepage).
	ProjectURLS []string
	// ServiceIcons maps service names to their respective icon identifiers or URLs.
	ServiceIcons map[string]string
}

// ParseArcaneComposeMetadata reads a Docker Compose file and extracts Arcane-specific metadata.
func ParseArcaneComposeMetadata(ctx context.Context, composeFilePath string) (ArcaneComposeMetadata, error) {
	workdir := filepath.Dir(composeFilePath)
	envMap := loadComposeEnvironment(workdir)
	return ParseArcaneComposeMetadataWithEnv(ctx, composeFilePath, envMap)
}

// ParseArcaneComposeMetadataWithEnv reads a Docker Compose file and extracts Arcane-specific metadata using a provided environment.
func ParseArcaneComposeMetadataWithEnv(ctx context.Context, composeFilePath string, envMap map[string]string) (ArcaneComposeMetadata, error) {
	return parseArcaneComposeMetadataFromFileInternal(ctx, composeFilePath, envMap, map[string]struct{}{})
}

func parseArcaneComposeMetadataFromFileInternal(ctx context.Context, composeFilePath string, envMap map[string]string, visited map[string]struct{}) (ArcaneComposeMetadata, error) {
	meta := ArcaneComposeMetadata{ServiceIcons: map[string]string{}}
	if composeFilePath == "" {
		return meta, nil
	}

	absPath, err := filepath.Abs(composeFilePath)
	if err != nil {
		absPath = composeFilePath
	}

	if _, seen := visited[absPath]; seen {
		return meta, nil
	}
	visited[absPath] = struct{}{}

	workdir := filepath.Dir(absPath)
	mergedEnv := mergeEnvFromDotEnv(envMap, workdir)

	project, err := loadComposeProjectForMetadataFromFileInternal(ctx, absPath, mergedEnv)
	if err != nil {
		return meta, fmt.Errorf("load compose metadata: %w", err)
	}

	meta = extractArcaneComposeMetadata(project)

	includePaths, err := parseIncludePaths(absPath)
	if err != nil {
		return meta, err
	}

	for _, includePath := range includePaths {
		if includePath == "" {
			continue
		}
		resolvedPath := includePath
		if !filepath.IsAbs(resolvedPath) {
			resolvedPath = filepath.Join(workdir, resolvedPath)
		}
		includedMeta, err := parseArcaneComposeMetadataFromFileInternal(ctx, resolvedPath, mergedEnv, visited)
		if err != nil {
			continue
		}
		mergeArcaneComposeMetadata(&meta, includedMeta)
	}

	return meta, nil
}

func extractArcaneComposeMetadata(project *composetypes.Project) ArcaneComposeMetadata {
	meta := ArcaneComposeMetadata{ServiceIcons: map[string]string{}}
	if project == nil {
		return meta
	}

	if arcaneBlock, ok := project.Extensions[arcaneBlockKey]; ok {
		meta.ProjectIconURL, meta.ProjectURLS = parseArcaneBlock(arcaneBlock)
	}

	for name, svc := range project.Services {
		icon := findArcaneIconLabel(svc.Labels)
		if icon == "" && svc.Deploy != nil {
			icon = findArcaneIconLabel(svc.Deploy.Labels)
		}
		if icon == "" {
			if arcaneBlock, ok := svc.Extensions[arcaneBlockKey]; ok {
				icon, _ = parseArcaneBlock(arcaneBlock)
			}
		}
		if icon != "" {
			meta.ServiceIcons[name] = icon
		}
	}

	return meta
}

func parseArcaneBlock(block any) (string, []string) {
	arcaneBlock, ok := utils.AsStringMap(block)
	if !ok {
		return "", nil
	}
	icon := utils.FirstNonEmpty(getFirstString(arcaneBlock[arcaneIconKey]), getFirstString(arcaneBlock[arcaneIconsKey]))
	urls := utils.UniqueNonEmptyStrings(utils.Collect(arcaneBlock[arcaneURLsKey], utils.ToString))
	return icon, urls
}

func mergeArcaneComposeMetadata(target *ArcaneComposeMetadata, source ArcaneComposeMetadata) {
	if target == nil {
		return
	}

	if target.ProjectIconURL == "" {
		target.ProjectIconURL = source.ProjectIconURL
	}

	target.ProjectURLS = utils.UniqueNonEmptyStrings(append(target.ProjectURLS, source.ProjectURLS...))

	if target.ServiceIcons == nil {
		target.ServiceIcons = map[string]string{}
	}
	for name, icon := range source.ServiceIcons {
		if _, exists := target.ServiceIcons[name]; !exists {
			target.ServiceIcons[name] = icon
		}
	}
}

func loadComposeProjectForMetadataFromFileInternal(ctx context.Context, composeFilePath string, envMap map[string]string) (*composetypes.Project, error) {
	return loadComposeProjectInternal(ctx, composeFilePath, "", "", false, nil, envMap, func(opts *loader.Options) {
		opts.SkipValidation = true
		opts.SkipConsistencyCheck = true
		opts.SkipResolveEnvironment = false
	})
}

func loadComposeEnvironment(workdir string) map[string]string {
	envMap := loadProcessEnv()
	if workdir == "" {
		return envMap
	}

	if absWorkdir, err := filepath.Abs(workdir); err == nil {
		envMap["PWD"] = absWorkdir
	} else {
		envMap["PWD"] = workdir
	}

	envPath := filepath.Join(workdir, ".env")
	info, err := os.Stat(envPath)
	if err != nil || info.IsDir() {
		return envMap
	}

	fileEnv, err := parseEnvFileWithContext(envPath, envMap)
	if err != nil {
		return envMap
	}

	for k, v := range fileEnv {
		if _, exists := envMap[k]; !exists {
			envMap[k] = v
		}
	}

	return envMap
}

func mergeEnvFromDotEnv(envMap map[string]string, workdir string) map[string]string {
	merged := make(map[string]string, len(envMap)+1)
	for k, v := range envMap {
		merged[k] = v
	}
	if workdir == "" {
		return merged
	}

	if absWorkdir, err := filepath.Abs(workdir); err == nil {
		merged["PWD"] = absWorkdir
	} else if _, ok := merged["PWD"]; !ok {
		merged["PWD"] = workdir
	}

	envPath := filepath.Join(workdir, ".env")
	info, err := os.Stat(envPath)
	if err != nil || info.IsDir() {
		return merged
	}

	fileEnv, err := parseEnvFileWithContext(envPath, merged)
	if err != nil {
		return merged
	}

	for k, v := range fileEnv {
		if _, exists := merged[k]; !exists {
			merged[k] = v
		}
	}

	return merged
}

func loadProcessEnv() map[string]string {
	envMap := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	return envMap
}

func parseIncludePaths(composeFilePath string) ([]string, error) {
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}

	composeData := map[string]interface{}{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}

	rawIncludes, ok := composeData["include"]
	if !ok {
		return nil, nil
	}

	var includeItems []any
	switch v := rawIncludes.(type) {
	case []any:
		includeItems = v
	case []string:
		for _, item := range v {
			includeItems = append(includeItems, item)
		}
	case string:
		includeItems = []any{v}
	default:
		return nil, nil
	}

	paths := make([]string, 0, len(includeItems))
	for _, item := range includeItems {
		switch v := item.(type) {
		case string:
			paths = append(paths, v)
		case map[string]interface{}:
			if p, ok := v["path"]; ok {
				switch pathValue := p.(type) {
				case string:
					paths = append(paths, pathValue)
				case []any:
					for _, entry := range pathValue {
						if s, ok := entry.(string); ok {
							paths = append(paths, s)
						}
					}
				case []string:
					paths = append(paths, pathValue...)
				}
			}
		}
	}

	return paths, nil
}

// getFirstString retrieves the first non-empty string from a value (single or slice).
func getFirstString(v any) string {
	for _, s := range utils.Collect(v, utils.ToString) {
		if s != "" {
			return s
		}
	}
	return ""
}

// findArcaneIconLabel attempts to locate an Arcane icon label within service labels.
// It supports both map[string]string and []string label formats.
func findArcaneIconLabel(labels any) string {
	if labelMap, ok := utils.AsStringMap(labels); ok {
		for key, value := range labelMap {
			if isArcaneIconLabel(key) {
				return utils.ToString(value)
			}
		}
	}

	for _, s := range utils.Collect(labels, utils.ToString) {
		if key, value, ok := parseLabelPair(s); ok && isArcaneIconLabel(key) {
			return value
		}
	}

	return ""
}

// parseLabelPair parses a "KEY=VALUE" string into its components.
func parseLabelPair(raw string) (string, string, bool) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// isArcaneIconLabel checks if a label key matches the Arcane icon label definition.
func isArcaneIconLabel(key string) bool {
	return strings.ToLower(strings.TrimSpace(key)) == ArcaneIconLabel
}
