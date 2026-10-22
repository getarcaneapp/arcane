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
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	updaterlabels "go.getarcane.app/updater/labels"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/imageref"
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
	// ProjectURLS are additional URLs related to the project (e.g., documentation, homepage).
	ProjectURLS []string
	// ProjectTags are normalized tags managed by Compose metadata.
	ProjectTags []types.ProjectTagOption
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
		return emptyArcaneComposeMetadata(), nil
	}

	workdir := filepath.Dir(composeFilePath)
	var envMap map[string]string
	if strings.TrimSpace(projectsDirectory) == "" {
		envMap = loadComposeEnvironment(workdir)
	} else {
		envLoader := NewEnvLoader(projectsDirectory, workdir, autoInjectEnv)
		loaded, _, err := envLoader.LoadEnvironment(ctx)
		if err != nil {
			return emptyArcaneComposeMetadata(), fmt.Errorf("load project environment: %w", err)
		}
		envMap = loaded
	}

	visited := map[string]struct{}{}
	meta, err := parseArcaneComposeMetadataFromFile(ctx, composeFilePath, envMap, visited)
	if err != nil {
		return meta, err
	}
	meta.ComposeFiles = kit.Unique(kit.TrimNonEmpty(append(meta.ComposeFiles, slices.Collect(maps.Keys(visited))...)))
	slices.Sort(meta.ComposeFiles)
	meta.EnvFiles = kit.Unique(kit.TrimNonEmpty(append(meta.EnvFiles, resolveComposeEnvFiles(workdir, envMap)...)))
	return meta, nil
}

// resolveComposeEnvFiles resolves the COMPOSE_ENV_FILES entries env
// declares against workdir, skipping entries that escape it, mirroring what
// EnvLoader merges for that directory.
func resolveComposeEnvFiles(workdir string, env EnvMap) []string {
	entries := ComposeEnvFileEntriesFromEnv(env)
	resolved := make([]string, 0, len(entries))
	for _, entry := range entries {
		if path, err := ResolvePathWithinDir(workdir, entry); err == nil {
			resolved = append(resolved, path)
		}
	}
	return resolved
}

func parseArcaneComposeMetadataFromFile(ctx context.Context, composeFilePath string, envMap map[string]string, visited map[string]struct{}) (ArcaneComposeMetadata, error) {
	meta := emptyArcaneComposeMetadata()
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

	project, err := loadComposeProjectForMetadataFromFile(ctx, absPath, mergedEnv)
	if err != nil {
		return meta, fmt.Errorf("load compose metadata: %w", err)
	}

	meta = extractArcaneComposeMetadata(ctx, project)
	if project != nil {
		meta.ComposeFiles = slices.Clone(project.ComposeFiles)
	}
	// The nested load's EnvLoader reads the sibling .env and the COMPOSE_ENV_FILES
	// it declares, even where the inherited environment overrides those values.
	meta.EnvFiles = append([]string{filepath.Join(workdir, EffectiveEnvFileName)}, resolveComposeEnvFiles(workdir, siblingEnv)...)

	// Resolve include paths with the environment the loader interpolated, including COMPOSE_ENV_FILES.
	includes, err := ParseIncludes(absPath, project.Environment, false)
	if err != nil {
		return meta, err
	}

	for _, include := range includes {
		includedMeta, parseArcaneComposeMetadataFromFileErr := parseArcaneComposeMetadataFromFile(ctx, include.Path, mergedEnv, visited)
		if parseArcaneComposeMetadataFromFileErr != nil {
			return meta, fmt.Errorf("load included Compose metadata %s: %w", include.Path, parseArcaneComposeMetadataFromFileErr)
		}
		mergeArcaneComposeMetadata(ctx, &meta, includedMeta)
	}

	return meta, nil
}

func extractArcaneComposeMetadata(ctx context.Context, project *composetypes.Project) ArcaneComposeMetadata {
	meta := emptyArcaneComposeMetadata()
	if project == nil {
		return meta
	}

	if arcaneBlock, ok := project.Extensions[arcaneBlockKey]; ok {
		meta.ProjectIcon, meta.ProjectURLS, meta.ProjectTags, meta.ProjectTagsAuthoritative = parseArcaneBlock(ctx, arcaneBlock)
	}

	for name, svc := range project.Services {
		iconSet := FindArcaneIconSet(svc.Labels)
		if iconSet.IsEmpty() && svc.Deploy != nil {
			iconSet = FindArcaneIconSet(svc.Deploy.Labels)
		}
		if iconSet.IsEmpty() {
			if arcaneBlock, ok := svc.Extensions[arcaneBlockKey]; ok {
				iconSet, _, _, _ = parseArcaneBlock(ctx, arcaneBlock)
			}
		}
		if !iconSet.IsEmpty() {
			meta.ServiceIconSets[name] = iconSet
		}
	}

	return meta
}

func parseArcaneBlock(ctx context.Context, block any) (IconSet, []string, []types.ProjectTagOption, bool) {
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
	urls := kit.Unique(kit.TrimNonEmpty(kit.Collect(arcaneBlock[arcaneURLsKey], kit.ToString)))
	tags, tagsAuthoritative := parseComposeTags(ctx, arcaneBlock[arcaneTagsKey])
	return icon, urls, tags, tagsAuthoritative
}

func parseComposeTags(ctx context.Context, value any) ([]types.ProjectTagOption, bool) {
	values, ok := value.([]any)
	if !ok {
		if value != nil {
			slog.WarnContext(ctx, "skipping invalid x-arcane tags; expected a list of name/color objects")
			return nil, false
		}
		return nil, true
	}

	tags := make([]types.ProjectTagOption, 0, min(len(values), ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(values))
	authoritative := true
	for index, value := range values {
		definition, localOk := kit.AsStringMap(value)
		if !localOk {
			slog.WarnContext(ctx, "skipping invalid x-arcane tag; expected a name/color object", "index", index)
			authoritative = false
			continue
		}
		nameValue := kit.ToString(definition["name"])
		colorValue := kit.ToString(definition["color"])
		if nameValue == "" || colorValue == "" {
			slog.WarnContext(ctx, "skipping invalid x-arcane tag; name and color are required", "index", index)
			authoritative = false
			continue
		}
		name, err := NormalizeProjectTag(nameValue)
		if err != nil {
			slog.WarnContext(ctx, "skipping invalid x-arcane tag", "index", index, "error", err)
			authoritative = false
			continue
		}
		color, err := NormalizeProjectTagColor(types.ProjectTagColor(colorValue))
		if err != nil {
			slog.WarnContext(ctx, "skipping invalid x-arcane tag color", "index", index, "tag", name, "error", err)
			authoritative = false
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		if len(tags) >= ProjectTagsPerSourceLimit {
			slog.WarnContext(ctx, "skipping x-arcane tags over per-project limit", "limit", ProjectTagsPerSourceLimit)
			authoritative = false
			break
		}
		seen[name] = struct{}{}
		tags = append(tags, types.ProjectTagOption{Name: name, Color: color})
	}
	return tags, authoritative
}

func mergeComposeTags(ctx context.Context, target, source []types.ProjectTagOption) []types.ProjectTagOption {
	merged := make([]types.ProjectTagOption, 0, min(len(target)+len(source), ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(target)+len(source))
	for _, tag := range append(target, source...) {
		if _, exists := seen[tag.Name]; exists {
			continue
		}
		if len(merged) >= ProjectTagsPerSourceLimit {
			slog.WarnContext(ctx, "skipping included x-arcane tags over per-project limit", "limit", ProjectTagsPerSourceLimit)
			break
		}
		seen[tag.Name] = struct{}{}
		merged = append(merged, tag)
	}
	return merged
}

func mergeArcaneComposeMetadata(ctx context.Context, target *ArcaneComposeMetadata, source ArcaneComposeMetadata) {
	if target == nil {
		return
	}

	target.ProjectIcon = mergeIconSetFields(target.ProjectIcon, source.ProjectIcon)

	target.ProjectURLS = kit.Unique(kit.TrimNonEmpty(append(target.ProjectURLS, source.ProjectURLS...)))
	target.ProjectTags = mergeComposeTags(ctx, target.ProjectTags, source.ProjectTags)
	target.ProjectTagsAuthoritative = target.ProjectTagsAuthoritative && source.ProjectTagsAuthoritative
	target.ComposeFiles = append(target.ComposeFiles, source.ComposeFiles...)
	target.EnvFiles = append(target.EnvFiles, source.EnvFiles...)

	if target.ServiceIconSets == nil {
		target.ServiceIconSets = map[string]IconSet{}
	}
	for name, iconSet := range source.ServiceIconSets {
		target.ServiceIconSets[name] = mergeIconSetFields(target.ServiceIconSets[name], iconSet)
	}
}

func mergeIconSetFields(target, source IconSet) IconSet {
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

func emptyArcaneComposeMetadata() ArcaneComposeMetadata {
	return ArcaneComposeMetadata{
		ProjectTagsAuthoritative: true,
		ServiceIconSets:          map[string]IconSet{},
	}
}

func loadComposeProjectForMetadataFromFile(ctx context.Context, composeFilePath string, envMap map[string]string) (*composetypes.Project, error) {
	return LoadComposeProject(ctx, composeFilePath, "", "", false, nil, envMap, func(opts *loader.Options) {
		opts.SkipValidation = true
		opts.SkipConsistencyCheck = true
		opts.SkipResolveEnvironment = false
	}, false, nil, nil, nil)
}

func loadComposeEnvironment(workdir string) map[string]string {
	envMap := allowedProcessEnv()
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

// FindArcaneIconSet attempts to locate Arcane icon labels within service labels.
// It supports both map[string]string and []string label formats.
func FindArcaneIconSet(labels any) IconSet {
	iconSet := IconSet{}
	if labelMap, ok := kit.AsStringMap(labels); ok {
		for key, value := range labelMap {
			assignArcaneIconValue(&iconSet, key, kit.ToString(value))
		}
		return iconSet
	}

	for _, s := range kit.Collect(labels, kit.ToString) {
		if key, value, ok := parseLabelPair(s); ok {
			assignArcaneIconValue(&iconSet, key, value)
		}
	}

	return iconSet
}

func assignArcaneIconValue(iconSet *IconSet, key, value string) {
	if iconSet == nil {
		return
	}

	switch normalizeArcaneIconLabel(key) {
	case "icon":
		iconSet.Icon = value
	case "icon-light":
		iconSet.Light = value
	case "icon-dark":
		iconSet.Dark = value
	}
}

func normalizeArcaneIconLabel(key string) string {
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

var projectTagColorsInternal = map[types.ProjectTagColor]struct{}{
	types.ProjectTagColorGray:   {},
	types.ProjectTagColorPurple: {},
	types.ProjectTagColorBlue:   {},
	types.ProjectTagColorGreen:  {},
	types.ProjectTagColorYellow: {},
	types.ProjectTagColorOrange: {},
	types.ProjectTagColorRed:    {},
	types.ProjectTagColorPink:   {},
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
func NormalizeProjectTagColor(color types.ProjectTagColor) (types.ProjectTagColor, error) {
	normalized := types.ProjectTagColor(strings.ToLower(strings.TrimSpace(string(color))))
	if normalized == "" {
		return types.ProjectTagColorGray, nil
	}
	if _, valid := projectTagColorsInternal[normalized]; !valid {
		return "", fmt.Errorf("unsupported tag color %q", color)
	}
	return normalized, nil
}

// applyServiceLabelMetadata translates Compose metadata into the labels the
// services consume, before previews or deployment use the model.
func applyServiceLabelMetadata(project *composetypes.Project) error {
	blockLabels := func(block any) (map[string]string, error) {
		labels, err := updaterMetadataLabels(block)
		if err != nil {
			return nil, fmt.Errorf("x-arcane.updater: %w", err)
		}
		arcane, _ := kit.AsStringMap(block)
		for _, metadata := range []struct {
			key   string
			label string
		}{
			{key: "hidden", label: libarcane.HiddenResourceLabel},
			{key: "update-check", label: imageref.UpdateCheckLabel},
		} {
			value, present := arcane[metadata.key]
			if !present {
				continue
			}
			enabled, boolErr := metadataBool(value)
			if boolErr != nil {
				return nil, fmt.Errorf("x-arcane.%s: %w", metadata.key, boolErr)
			}
			labels[metadata.label] = strconv.FormatBool(enabled)
		}
		return labels, nil
	}

	defaults, err := blockLabels(project.Extensions[arcaneBlockKey])
	if err != nil {
		return err
	}
	for name, service := range project.Services {
		overrides, overridesErr := blockLabels(service.Extensions[arcaneBlockKey])
		if overridesErr != nil {
			return fmt.Errorf("service %s %w", name, overridesErr)
		}
		if len(defaults)+len(overrides) == 0 {
			continue
		}
		labels := composetypes.Labels{}
		maps.Copy(labels, defaults)
		maps.Copy(labels, overrides)
		maps.Copy(labels, service.Labels)
		service.Labels = labels
		project.Services[name] = service
	}
	return nil
}

func updaterMetadataLabels(block any) (map[string]string, error) {
	arcane, ok := kit.AsStringMap(block)
	if !ok {
		return map[string]string{}, nil
	}
	raw, present := arcane["updater"]
	if !present {
		return map[string]string{}, nil
	}
	config, ok := kit.AsStringMap(raw)
	if !ok {
		return nil, errors.New("expected an updater mapping")
	}
	result := make(map[string]string, len(config))
	for key, value := range config {
		if key == "enabled" {
			enabled, err := metadataBool(value)
			if err != nil {
				return nil, err
			}
			result[updaterlabels.LabelUpdater] = strconv.FormatBool(enabled)
			continue
		}
		if key == "delay" {
			hours, err := metadataHours(value)
			if err != nil {
				return nil, err
			}
			result[imageref.UpdateDelayLabel] = strconv.Itoa(hours)
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

// metadataHours accepts a whole, non-negative number of hours. YAML decoders
// may surface it as any numeric type or as a numeric string.
func metadataHours(value any) (int, error) {
	hours, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || hours < 0 || hours > imageref.MaxUpdateDelayHours {
		return 0, fmt.Errorf("updater delay must be a whole number of hours between 0 and %d", imageref.MaxUpdateDelayHours)
	}
	return hours, nil
}

func metadataBool(value any) (bool, error) {
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
