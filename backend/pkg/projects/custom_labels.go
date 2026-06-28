// Package projects provides utilities for managing Docker Compose projects and their metadata.
package projects

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"go.yaml.in/yaml/v4"
)

const (
	// ArcaneIconLabel is the full reverse-DNS label key for fallback service-level icons.
	ArcaneIconLabel = "com.getarcaneapp.arcane.icon"
	// ArcaneIconLightLabel is the full reverse-DNS label key for light service-level icons.
	ArcaneIconLightLabel = "com.getarcaneapp.arcane.icon-light"
	// ArcaneIconDarkLabel is the full reverse-DNS label key for dark service-level icons.
	ArcaneIconDarkLabel = "com.getarcaneapp.arcane.icon-dark"

	arcaneBlockKey     = "x-arcane"
	arcaneIconKey      = "icon"
	arcaneIconsKey     = "icons"
	arcaneIconLightKey = "icon-light"
	arcaneIconDarkKey  = "icon-dark"
	arcaneURLsKey      = "urls"
)

type IconSet = iconcatalog.IconSet

// ArcaneComposeMetadata represents Arcane-specific configuration extracted from a Compose file.
type ArcaneComposeMetadata struct {
	// ProjectIcon contains fallback, light, and dark icon values for the project.
	ProjectIcon IconSet
	// ProjectURLS are additional URLs related to the project (e.g., documentation, homepage).
	ProjectURLS []string
	// ServiceIconSets maps service names to their fallback, light, and dark icon values.
	ServiceIconSets map[string]IconSet
}

// ParseArcaneComposeMetadata reads a Docker Compose file and extracts Arcane-specific metadata.
// When projectsDirectory is set, Arcane's project env loading is used so .env.global is available.
func ParseArcaneComposeMetadata(ctx context.Context, composeFilePath, projectsDirectory string, autoInjectEnv bool) (ArcaneComposeMetadata, error) {
	if composeFilePath == "" {
		return emptyArcaneComposeMetadataInternal(), nil
	}

	workdir := filepath.Dir(composeFilePath)
	if strings.TrimSpace(projectsDirectory) == "" {
		envMap := loadComposeEnvironment(workdir)
		return ParseArcaneComposeMetadataWithEnv(ctx, composeFilePath, envMap)
	}

	envLoader := NewEnvLoader(projectsDirectory, workdir, autoInjectEnv)
	envMap, _, err := envLoader.LoadEnvironment(ctx)
	if err != nil {
		return emptyArcaneComposeMetadataInternal(), fmt.Errorf("load project environment: %w", err)
	}

	return ParseArcaneComposeMetadataWithEnv(ctx, composeFilePath, envMap)
}

// ParseArcaneComposeMetadataWithEnv reads a Docker Compose file and extracts Arcane-specific metadata using a provided environment.
func ParseArcaneComposeMetadataWithEnv(ctx context.Context, composeFilePath string, envMap map[string]string) (ArcaneComposeMetadata, error) {
	return parseArcaneComposeMetadataFromFileInternal(ctx, composeFilePath, envMap, map[string]struct{}{})
}

func parseArcaneComposeMetadataFromFileInternal(ctx context.Context, composeFilePath string, envMap map[string]string, visited map[string]struct{}) (ArcaneComposeMetadata, error) {
	meta := emptyArcaneComposeMetadataInternal()
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
	meta := emptyArcaneComposeMetadataInternal()
	if project == nil {
		return meta
	}

	if arcaneBlock, ok := project.Extensions[arcaneBlockKey]; ok {
		meta.ProjectIcon, meta.ProjectURLS = parseArcaneBlockInternal(arcaneBlock)
	}

	for name, svc := range project.Services {
		iconSet := FindArcaneIconSet(svc.Labels)
		if iconSet.IsEmpty() && svc.Deploy != nil {
			iconSet = FindArcaneIconSet(svc.Deploy.Labels)
		}
		if iconSet.IsEmpty() {
			if arcaneBlock, ok := svc.Extensions[arcaneBlockKey]; ok {
				iconSet, _ = parseArcaneBlockInternal(arcaneBlock)
			}
		}
		if !iconSet.IsEmpty() {
			meta.ServiceIconSets[name] = iconSet
		}
	}

	return meta
}

func parseArcaneBlockInternal(block any) (IconSet, []string) {
	arcaneBlock, ok := utils.AsStringMap(block)
	if !ok {
		return IconSet{}, nil
	}
	icon := IconSet{
		Icon:  utils.FirstNonEmpty(getFirstString(arcaneBlock[arcaneIconKey]), getFirstString(arcaneBlock[arcaneIconsKey])),
		Light: getFirstString(arcaneBlock[arcaneIconLightKey]),
		Dark:  getFirstString(arcaneBlock[arcaneIconDarkKey]),
	}
	urls := utils.UniqueNonEmptyStrings(utils.Collect(arcaneBlock[arcaneURLsKey], utils.ToString))
	return icon, urls
}

func mergeArcaneComposeMetadata(target *ArcaneComposeMetadata, source ArcaneComposeMetadata) {
	if target == nil {
		return
	}

	target.ProjectIcon = mergeIconSetFieldsInternal(target.ProjectIcon, source.ProjectIcon)

	target.ProjectURLS = utils.UniqueNonEmptyStrings(append(target.ProjectURLS, source.ProjectURLS...))

	if target.ServiceIconSets == nil {
		target.ServiceIconSets = map[string]IconSet{}
	}
	for name, iconSet := range source.ServiceIconSets {
		target.ServiceIconSets[name] = mergeIconSetFieldsInternal(target.ServiceIconSets[name], iconSet)
	}
}

func mergeIconSetFieldsInternal(target, source IconSet) IconSet {
	if strings.TrimSpace(target.Icon) == "" {
		target.Icon = source.Icon
	}
	if strings.TrimSpace(target.Light) == "" {
		target.Light = source.Light
	}
	if strings.TrimSpace(target.Dark) == "" {
		target.Dark = source.Dark
	}
	return target
}

func emptyArcaneComposeMetadataInternal() ArcaneComposeMetadata {
	return ArcaneComposeMetadata{
		ServiceIconSets: map[string]IconSet{},
	}
}

func loadComposeProjectForMetadataFromFileInternal(ctx context.Context, composeFilePath string, envMap map[string]string) (*composetypes.Project, error) {
	return loadComposeProjectInternal(ctx, composeFilePath, "", "", false, nil, envMap, func(opts *loader.Options) {
		opts.SkipValidation = true
		opts.SkipConsistencyCheck = true
		opts.SkipResolveEnvironment = false
	}, false)
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

	fileEnv, err := ParseProjectEnvFile(envPath, envMap)
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
	maps.Copy(merged, envMap)
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

	fileEnv, err := ParseProjectEnvFile(envPath, merged)
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

	composeData := map[string]any{}
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
		case map[string]any:
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

// FindArcaneIconSet attempts to locate Arcane icon labels within service labels.
// It supports both map[string]string and []string label formats.
func FindArcaneIconSet(labels any) IconSet {
	iconSet := IconSet{}
	if labelMap, ok := utils.AsStringMap(labels); ok {
		for key, value := range labelMap {
			assignArcaneIconValueInternal(&iconSet, key, utils.ToString(value))
		}
		return iconSet
	}

	for _, s := range utils.Collect(labels, utils.ToString) {
		if key, value, ok := parseLabelPair(s); ok {
			assignArcaneIconValueInternal(&iconSet, key, value)
		}
	}

	return iconSet
}

func assignArcaneIconValueInternal(iconSet *IconSet, key string, value string) {
	if iconSet == nil {
		return
	}

	switch normalizeArcaneIconLabelInternal(key) {
	case "icon":
		iconSet.Icon = value
	case "icon-light":
		iconSet.Light = value
	case "icon-dark":
		iconSet.Dark = value
	}
}

func normalizeArcaneIconLabelInternal(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case ArcaneIconLabel, "arcane.icon":
		return "icon"
	case ArcaneIconLightLabel, "arcane.icon-light":
		return "icon-light"
	case ArcaneIconDarkLabel, "arcane.icon-dark":
		return "icon-dark"
	default:
		return ""
	}
}

// parseLabelPair parses a "KEY=VALUE" string into its components.
func parseLabelPair(raw string) (string, string, bool) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}
