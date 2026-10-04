package projects

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	kit "go.getarcane.app/kit/pkg"
	updaterlabels "go.getarcane.app/updater/labels"
	"go.yaml.in/yaml/v4"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
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
	arcaneTagsKey      = "tags"

	// ProjectTagMaxLength is the maximum number of Unicode characters in a tag name.
	ProjectTagMaxLength = 64
	// ProjectTagsPerSourceLimit is the maximum number of UI or Compose tags on one project.
	ProjectTagsPerSourceLimit = 50
)

type IconSet = iconcatalog.IconSet

// ArcaneComposeMetadata represents Arcane-specific configuration extracted from a Compose file.
type ArcaneComposeMetadata struct {
	// ProjectIcon contains fallback, light, and dark icon values for the project.
	ProjectIcon IconSet
	// ProjectLinks are additional labeled URLs related to the project.
	ProjectLinks []projecttypes.Link
	// ProjectTags are normalized tags managed by Compose metadata.
	ProjectTags []projecttypes.TagOption
	// ProjectTagsAuthoritative reports whether every Compose tag was parsed successfully.
	ProjectTagsAuthoritative bool
	// ServiceIconSets maps service names to their fallback, light, and dark icon values.
	ServiceIconSets map[string]IconSet
	// ComposeFiles lists every compose file the metadata was parsed from: the root, COMPOSE_FILE entries, an auto-loaded override, and includes.
	ComposeFiles []string
	// EnvFiles lists every env file the parse read: the .env beside each parsed compose file plus resolved COMPOSE_ENV_FILES entries.
	EnvFiles []string
}

// ParseArcaneComposeMetadata reads a Docker Compose file and extracts Arcane-specific metadata.
// When projectsDirectory is set, Arcane's project env loading is used so .env.global is available.
func ParseArcaneComposeMetadata(ctx context.Context, composeFilePath, projectsDirectory string, autoInjectEnv bool) (ArcaneComposeMetadata, error) {
	if composeFilePath == "" {
		return emptyArcaneComposeMetadataInternal(), nil
	}

	workdir := filepath.Dir(composeFilePath)
	var envMap map[string]string
	if strings.TrimSpace(projectsDirectory) == "" {
		envMap = loadComposeEnvironment(workdir)
	} else {
		envLoader := NewEnvLoader(projectsDirectory, workdir, autoInjectEnv)
		loaded, _, err := envLoader.LoadEnvironment(ctx)
		if err != nil {
			return emptyArcaneComposeMetadataInternal(), fmt.Errorf("load project environment: %w", err)
		}
		envMap = loaded
	}

	visited := map[string]struct{}{}
	meta, err := parseArcaneComposeMetadataFromFileInternal(ctx, composeFilePath, envMap, visited)
	if err != nil {
		return meta, err
	}
	meta.ComposeFiles = kit.Unique(kit.TrimNonEmpty(append(meta.ComposeFiles, slices.Collect(maps.Keys(visited))...)))
	slices.Sort(meta.ComposeFiles)
	meta.EnvFiles = kit.Unique(kit.TrimNonEmpty(append(meta.EnvFiles, resolveComposeEnvFilesInternal(workdir, envMap)...)))
	return meta, nil
}

// resolveComposeEnvFilesInternal resolves the COMPOSE_ENV_FILES entries env
// declares against workdir, skipping entries that escape it, mirroring what
// EnvLoader merges for that directory.
func resolveComposeEnvFilesInternal(workdir string, env EnvMap) []string {
	entries := ComposeEnvFileEntriesFromEnv(env)
	resolved := make([]string, 0, len(entries))
	for _, entry := range entries {
		if path, err := ResolvePathWithinDir(workdir, entry); err == nil {
			resolved = append(resolved, path)
		}
	}
	return resolved
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
	mergedEnv, siblingEnv := mergeEnvFromDotEnv(envMap, workdir)

	project, err := loadComposeProjectForMetadataFromFileInternal(ctx, absPath, mergedEnv)
	if err != nil {
		return meta, fmt.Errorf("load compose metadata: %w", err)
	}

	meta = extractArcaneComposeMetadata(project)
	if project != nil {
		meta.ComposeFiles = slices.Clone(project.ComposeFiles)
	}
	// The nested load's EnvLoader reads the sibling .env and the COMPOSE_ENV_FILES
	// it declares, even where the inherited environment overrides those values.
	meta.EnvFiles = append([]string{filepath.Join(workdir, EffectiveEnvFileName)}, resolveComposeEnvFilesInternal(workdir, siblingEnv)...)

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
		includedMeta, parseArcaneComposeMetadataFromFileErr := parseArcaneComposeMetadataFromFileInternal(ctx, resolvedPath, mergedEnv, visited)
		if parseArcaneComposeMetadataFromFileErr != nil {
			return meta, fmt.Errorf("load included Compose metadata %s: %w", resolvedPath, parseArcaneComposeMetadataFromFileErr)
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
		meta.ProjectIcon, meta.ProjectLinks, meta.ProjectTags, meta.ProjectTagsAuthoritative = parseArcaneBlockInternal(arcaneBlock)
	}

	for name, svc := range project.Services {
		iconSet := FindArcaneIconSet(svc.Labels)
		if iconSet.IsEmpty() && svc.Deploy != nil {
			iconSet = FindArcaneIconSet(svc.Deploy.Labels)
		}
		if iconSet.IsEmpty() {
			if arcaneBlock, ok := svc.Extensions[arcaneBlockKey]; ok {
				iconSet, _, _, _ = parseArcaneBlockInternal(arcaneBlock)
			}
		}
		if !iconSet.IsEmpty() {
			meta.ServiceIconSets[name] = iconSet
		}
	}

	return meta
}

func parseArcaneBlockInternal(block any) (IconSet, []projecttypes.Link, []projecttypes.TagOption, bool) {
	arcaneBlock, ok := kit.AsStringMap(block)
	if !ok {
		return IconSet{}, nil, nil, false
	}
	icon := IconSet{
		Icon: cmp.Or(
			cmp.Or(kit.Collect(arcaneBlock[arcaneIconKey], kit.ToString)...),
			cmp.Or(kit.Collect(arcaneBlock[arcaneIconsKey], kit.ToString)...),
		),
		Light: cmp.Or(kit.Collect(arcaneBlock[arcaneIconLightKey], kit.ToString)...),
		Dark:  cmp.Or(kit.Collect(arcaneBlock[arcaneIconDarkKey], kit.ToString)...),
	}
	links := parseProjectLinksInternal(arcaneBlock[arcaneURLsKey])
	tags, tagsAuthoritative := parseComposeTagsInternal(arcaneBlock[arcaneTagsKey])
	return icon, links, tags, tagsAuthoritative
}

func parseProjectLinksInternal(value any) []projecttypes.Link {
	links := kit.Collect(value, func(entry any) projecttypes.Link {
		if url, ok := entry.(string); ok {
			return projecttypes.Link{URL: strings.TrimSpace(url)}
		}
		fields, ok := kit.AsStringMap(entry)
		if !ok {
			slog.Warn("skipping invalid x-arcane URL; expected a string or URL object")
			return projecttypes.Link{}
		}
		url, validURL := fields["url"].(string)
		label, validLabel := fields["label"].(string)
		if !validURL || (fields["label"] != nil && !validLabel) {
			slog.Warn("skipping invalid x-arcane URL; url and label must be strings")
			return projecttypes.Link{}
		}
		return projecttypes.Link{URL: strings.TrimSpace(url), Label: strings.TrimSpace(label)}
	})
	return mergeProjectLinksInternal(nil, links)
}

func mergeProjectLinksInternal(target, source []projecttypes.Link) []projecttypes.Link {
	seen := make(map[string]struct{}, len(target)+len(source))
	for _, link := range target {
		seen[link.URL] = struct{}{}
	}
	for _, link := range source {
		if link.URL == "" {
			slog.Warn("skipping empty x-arcane URL")
			continue
		}
		if _, exists := seen[link.URL]; exists {
			continue
		}
		seen[link.URL] = struct{}{}
		target = append(target, link)
	}
	return target
}

func parseComposeTagsInternal(value any) ([]projecttypes.TagOption, bool) {
	values, ok := value.([]any)
	if !ok {
		if value != nil {
			slog.Warn("skipping invalid x-arcane tags; expected a list of name/color objects")
			return nil, false
		}
		return nil, true
	}

	tags := make([]projecttypes.TagOption, 0, min(len(values), ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(values))
	authoritative := true
	for index, value := range values {
		definition, localOk := kit.AsStringMap(value)
		if !localOk {
			slog.Warn("skipping invalid x-arcane tag; expected a name/color object", "index", index)
			authoritative = false
			continue
		}
		nameValue := kit.ToString(definition["name"])
		colorValue := kit.ToString(definition["color"])
		if nameValue == "" || colorValue == "" {
			slog.Warn("skipping invalid x-arcane tag; name and color are required", "index", index)
			authoritative = false
			continue
		}
		name, err := NormalizeProjectTag(nameValue)
		if err != nil {
			slog.Warn("skipping invalid x-arcane tag", "index", index, "error", err)
			authoritative = false
			continue
		}
		color, err := NormalizeProjectTagColor(projecttypes.TagColor(colorValue))
		if err != nil {
			slog.Warn("skipping invalid x-arcane tag color", "index", index, "tag", name, "error", err)
			authoritative = false
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		if len(tags) >= ProjectTagsPerSourceLimit {
			slog.Warn("skipping x-arcane tags over per-project limit", "limit", ProjectTagsPerSourceLimit)
			authoritative = false
			break
		}
		seen[name] = struct{}{}
		tags = append(tags, projecttypes.TagOption{Name: name, Color: color})
	}
	return tags, authoritative
}

func mergeComposeTagsInternal(target, source []projecttypes.TagOption) []projecttypes.TagOption {
	merged := make([]projecttypes.TagOption, 0, min(len(target)+len(source), ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(target)+len(source))
	for _, tag := range append(target, source...) {
		if _, exists := seen[tag.Name]; exists {
			continue
		}
		if len(merged) >= ProjectTagsPerSourceLimit {
			slog.Warn("skipping included x-arcane tags over per-project limit", "limit", ProjectTagsPerSourceLimit)
			break
		}
		seen[tag.Name] = struct{}{}
		merged = append(merged, tag)
	}
	return merged
}

func mergeArcaneComposeMetadata(target *ArcaneComposeMetadata, source ArcaneComposeMetadata) {
	if target == nil {
		return
	}

	target.ProjectIcon = mergeIconSetFieldsInternal(target.ProjectIcon, source.ProjectIcon)

	target.ProjectLinks = mergeProjectLinksInternal(target.ProjectLinks, source.ProjectLinks)
	target.ProjectTags = mergeComposeTagsInternal(target.ProjectTags, source.ProjectTags)
	target.ProjectTagsAuthoritative = target.ProjectTagsAuthoritative && source.ProjectTagsAuthoritative
	target.ComposeFiles = append(target.ComposeFiles, source.ComposeFiles...)
	target.EnvFiles = append(target.EnvFiles, source.EnvFiles...)

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
		ProjectTagsAuthoritative: true,
		ServiceIconSets:          map[string]IconSet{},
	}
}

func loadComposeProjectForMetadataFromFileInternal(ctx context.Context, composeFilePath string, envMap map[string]string) (*composetypes.Project, error) {
	return LoadComposeProject(ctx, composeFilePath, "", "", false, nil, envMap, func(opts *loader.Options) {
		opts.SkipValidation = true
		opts.SkipConsistencyCheck = true
		opts.SkipResolveEnvironment = false
	}, false, nil, nil, nil)
}

func loadComposeEnvironment(workdir string) map[string]string {
	envMap := allowedProcessEnvInternal()
	if workdir == "" {
		return envMap
	}

	if absWorkdir, err := filepath.Abs(workdir); err == nil {
		envMap["PWD"] = absWorkdir
	} else {
		envMap["PWD"] = workdir
	}

	envPath := filepath.Join(workdir, ".env")
	// os.Stat rather than acfs: .env may be a symlink resolving outside any
	// confinement root (a supported setup).
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

// mergeEnvFromDotEnv layers workdir's .env under envMap (existing keys win) and
// also returns the .env values on their own.
func mergeEnvFromDotEnv(envMap map[string]string, workdir string) (map[string]string, EnvMap) {
	merged := make(map[string]string, len(envMap)+1)
	maps.Copy(merged, envMap)
	if workdir == "" {
		return merged, nil
	}

	if absWorkdir, err := filepath.Abs(workdir); err == nil {
		merged["PWD"] = absWorkdir
	} else if _, ok := merged["PWD"]; !ok {
		merged["PWD"] = workdir
	}

	envPath := filepath.Join(workdir, ".env")
	// os.Stat rather than acfs: .env may be a symlink resolving outside any
	// confinement root (a supported setup).
	info, err := os.Stat(envPath)
	if err != nil || info.IsDir() {
		return merged, nil
	}

	fileEnv, err := ParseProjectEnvFile(envPath, merged)
	if err != nil {
		return merged, nil
	}

	for k, v := range fileEnv {
		if _, exists := merged[k]; !exists {
			merged[k] = v
		}
	}

	return merged, fileEnv
}

func parseIncludePaths(composeFilePath string) ([]string, error) {
	// os.ReadFile rather than acfs: compose files may be symlinks resolving
	// outside any confinement root, including imported projects.
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}

	composeData := map[string]any{}
	if unmarshalErr := yaml.Unmarshal(content, &composeData); unmarshalErr != nil {
		return nil, fmt.Errorf("parse compose file: %w", unmarshalErr)
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
			if p, localOk := v["path"]; localOk {
				switch pathValue := p.(type) {
				case string:
					paths = append(paths, pathValue)
				case []any:
					for _, entry := range pathValue {
						if s, localOk2 := entry.(string); localOk2 {
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

// FindArcaneIconSet attempts to locate Arcane icon labels within service labels.
// It supports both map[string]string and []string label formats.
func FindArcaneIconSet(labels any) IconSet {
	iconSet := IconSet{}
	if labelMap, ok := kit.AsStringMap(labels); ok {
		for key, value := range labelMap {
			assignArcaneIconValueInternal(&iconSet, key, kit.ToString(value))
		}
		return iconSet
	}

	for _, s := range kit.Collect(labels, kit.ToString) {
		if key, value, ok := parseLabelPair(s); ok {
			assignArcaneIconValueInternal(&iconSet, key, value)
		}
	}

	return iconSet
}

func assignArcaneIconValueInternal(iconSet *IconSet, key, value string) {
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

var projectTagColorsInternal = map[projecttypes.TagColor]struct{}{
	projecttypes.TagColorGray:   {},
	projecttypes.TagColorPurple: {},
	projecttypes.TagColorBlue:   {},
	projecttypes.TagColorGreen:  {},
	projecttypes.TagColorYellow: {},
	projecttypes.TagColorOrange: {},
	projecttypes.TagColorRed:    {},
	projecttypes.TagColorPink:   {},
}

// NormalizeProjectTag validates and normalizes a project tag name.
func NormalizeProjectTag(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", errors.New("tag name cannot be empty")
	}
	if len([]rune(normalized)) > ProjectTagMaxLength {
		return "", fmt.Errorf("tag name cannot exceed %d characters", ProjectTagMaxLength)
	}
	if strings.ContainsRune(normalized, ',') {
		return "", errors.New("tag name cannot contain commas")
	}
	for _, char := range normalized {
		if unicode.IsControl(char) {
			return "", errors.New("tag name cannot contain control characters")
		}
	}
	return normalized, nil
}

// NormalizeProjectTags validates, normalizes, and de-duplicates project tags.
func NormalizeProjectTags(names []string) ([]string, error) {
	result := make([]string, 0, min(len(names), ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		normalized, err := NormalizeProjectTag(name)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		if len(result) >= ProjectTagsPerSourceLimit {
			return nil, fmt.Errorf("a project cannot have more than %d tags per source", ProjectTagsPerSourceLimit)
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

// NormalizeProjectTagColor validates and normalizes a project tag color.
func NormalizeProjectTagColor(color projecttypes.TagColor) (projecttypes.TagColor, error) {
	normalized := projecttypes.TagColor(strings.ToLower(strings.TrimSpace(string(color))))
	if normalized == "" {
		return projecttypes.TagColorGray, nil
	}
	if _, valid := projectTagColorsInternal[normalized]; !valid {
		return "", fmt.Errorf("unsupported tag color %q", color)
	}
	return normalized, nil
}

// applyServiceLabelMetadataInternal translates Compose metadata into the labels the
// services consume, before previews or deployment use the model.
func applyServiceLabelMetadataInternal(project *composetypes.Project) error {
	defaults, err := updaterMetadataLabelsInternal(project.Extensions[arcaneBlockKey])
	if err != nil {
		return fmt.Errorf("x-arcane.updater: %w", err)
	}
	hiddenDefaults, err := hiddenMetadataLabelInternal(project.Extensions[arcaneBlockKey])
	if err != nil {
		return fmt.Errorf("x-arcane.hidden: %w", err)
	}
	if defaults == nil {
		defaults = map[string]string{}
	}
	maps.Copy(defaults, hiddenDefaults)
	for name, service := range project.Services {
		overrides, updaterMetadataLabelsErr := updaterMetadataLabelsInternal(service.Extensions[arcaneBlockKey])
		if updaterMetadataLabelsErr != nil {
			return fmt.Errorf("service %s x-arcane.updater: %w", name, updaterMetadataLabelsErr)
		}
		effective := maps.Clone(defaults)
		hiddenOverrides, updaterMetadataLabelsErr := hiddenMetadataLabelInternal(service.Extensions[arcaneBlockKey])
		if updaterMetadataLabelsErr != nil {
			return fmt.Errorf("service %s x-arcane.hidden: %w", name, updaterMetadataLabelsErr)
		}
		maps.Copy(effective, overrides)
		maps.Copy(effective, hiddenOverrides)
		if len(effective) == 0 {
			continue
		}
		service.Labels = maps.Clone(service.Labels)
		if service.Labels == nil {
			service.Labels = composetypes.Labels{}
		}
		for key, value := range effective {
			if _, explicit := service.Labels[key]; !explicit {
				service.Labels[key] = value
			}
		}
		project.Services[name] = service
	}
	return nil
}

func updaterMetadataLabelsInternal(block any) (map[string]string, error) {
	arcane, ok := kit.AsStringMap(block)
	if !ok {
		return nil, nil
	}
	raw, present := arcane["updater"]
	if !present {
		return nil, nil
	}
	config, ok := kit.AsStringMap(raw)
	if !ok {
		return nil, errors.New("expected an updater mapping")
	}
	result := make(map[string]string, len(config))
	for key, value := range config {
		if key == "enabled" {
			enabled, err := metadataBoolInternal(value)
			if err != nil {
				return nil, err
			}
			result[updaterlabels.LabelUpdater] = strconv.FormatBool(enabled)
			continue
		}
		var label string
		switch key {
		case "strategy":
			label = updaterlabels.LabelUpdateStrategy
		case "constraint":
			label = updaterlabels.LabelUpdateConstraint
		case "tag-pattern":
			label = updaterlabels.LabelUpdateTagPattern
		default:
			return nil, fmt.Errorf("unknown updater option %q", key)
		}
		text, localOk := value.(string)
		if !localOk {
			return nil, fmt.Errorf("updater %s must be a string", key)
		}
		text = strings.TrimSpace(text)
		if key == "strategy" && text != "auto" && text != "tag" && text != "digest" {
			return nil, fmt.Errorf("unknown updater strategy %q", text)
		}
		result[label] = text
	}
	return result, nil
}

func hiddenMetadataLabelInternal(block any) (map[string]string, error) {
	arcane, ok := kit.AsStringMap(block)
	if !ok {
		return nil, nil
	}
	value, present := arcane["hidden"]
	if !present {
		return nil, nil
	}
	hidden, err := metadataBoolInternal(value)
	if err != nil {
		return nil, err
	}
	return map[string]string{libarcane.HiddenResourceLabel: strconv.FormatBool(hidden)}, nil
}

func metadataBoolInternal(value any) (bool, error) {
	if enabled, ok := value.(bool); ok {
		return enabled, nil
	}
	if text, ok := value.(string); ok {
		enabled, err := strconv.ParseBool(strings.TrimSpace(text))
		if err == nil {
			return enabled, nil
		}
	}
	return false, errors.New("expected a boolean")
}
