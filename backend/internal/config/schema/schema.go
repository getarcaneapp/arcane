package schema

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	kit "kit.getarcane.app"

	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
)

type (
	// SchemaDocument is the canonical JSON artifact consumed by the docs site.
	SchemaDocument struct {
		RenamedEnv    map[string]string `json:"renamedEnv"`
		EnvConfig     []ConfigEntry     `json:"envConfig"`
		Settings      []SettingEntry    `json:"settings"`
		RetiredEnv    []string          `json:"retiredEnv"`
		SchemaVersion int               `json:"schemaVersion"`
	}

	// ConfigEntry describes an environment-backed runtime config value.
	ConfigEntry struct {
		Env          string   `json:"env"`
		Field        string   `json:"field"`
		Type         string   `json:"type"`
		DefaultValue string   `json:"defaultValue,omitempty"`
		Description  string   `json:"description,omitempty"`
		Source       string   `json:"source"`
		SourceFile   string   `json:"sourceFile"`
		SourceSymbol string   `json:"sourceSymbol"`
		Options      []string `json:"options,omitempty"`
		BuildTags    []string `json:"buildTags,omitempty"`
		SupportsFile bool     `json:"supportsFile,omitempty"`
		Conditional  bool     `json:"conditional,omitempty"`
	}

	SettingEntry struct {
		Min          *float64 `json:"min,omitempty"`
		Max          *float64 `json:"max,omitempty"`
		Env          string   `json:"env"`
		Key          string   `json:"key"`
		Type         string   `json:"type"`
		Default      string   `json:"default,omitempty"`
		Scope        string   `json:"scope"`
		Description  string   `json:"description,omitempty"`
		Category     string   `json:"category,omitempty"`
		Requires     string   `json:"requires,omitempty"`
		Note         string   `json:"note,omitempty"`
		Source       string   `json:"source"`
		SourceFile   string   `json:"sourceFile"`
		SourceSymbol string   `json:"sourceSymbol"`
		Values       []string `json:"values,omitempty"`
		Sensitive    bool     `json:"sensitive"`
		Public       bool     `json:"public"`
	}

	envFieldOptions struct {
		sourceFile  string
		sourceType  string
		buildTags   []string
		conditional bool
	}

	overrideDocRule struct {
		requires string
		note     string
	}
)

const (
	schemaVersion                = 2
	sourceFileConfig             = "backend/internal/config/config.go"
	sourceFileBuildablesConfig   = "backend/internal/config/buildables_config.go"
	sourceFileSettings           = "backend/internal/settings/model.go"
	sourceSymbolConfig           = "config.Config"
	sourceSymbolBuildablesConfig = "config.BuildablesConfig"
)

var overrideDocRules = map[string]overrideDocRule{
	"autoUpdateInterval": {
		requires: "ARCANE_AUTO_UPDATE=true to have effect at runtime.",
	},
	"autoUpdateDelay": {
		requires: "ARCANE_AUTO_UPDATE=true to have effect at runtime.",
	},
	"scheduledPruneInterval": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"pruneContainerMode": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"pruneContainerUntil": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true and ARCANE_PRUNE_CONTAINER_MODE=olderThan to have effect at runtime.",
	},
	"pruneImageMode": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"pruneImageUntil": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true and ARCANE_PRUNE_IMAGE_MODE=olderThan to have effect at runtime.",
	},
	"pruneVolumeMode": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"pruneNetworkMode": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"pruneNetworkUntil": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true and ARCANE_PRUNE_NETWORK_MODE=olderThan to have effect at runtime.",
	},
	"pruneBuildCacheMode": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"pruneBuildCacheUntil": {
		requires: "ARCANE_SCHEDULED_PRUNE_ENABLED=true and ARCANE_PRUNE_BUILD_CACHE_MODE=olderThan to have effect at runtime.",
	},
	"featureVulnerabilityManagementEnabled": {
		note: "Disabling retains existing reports and scanner settings, allows active work to finish, and leaves standalone image patching available.",
	},
	"featureSwarmEnabled": {
		note: "An active swarm cluster keeps Swarm enabled regardless of this value.",
	},
	"vulnerabilityScanInterval": {
		requires: "ARCANE_FEATURE_VULNERABILITY_MANAGEMENT_ENABLED=true and ARCANE_VULNERABILITY_SCAN_ENABLED=true to have effect at runtime.",
	},
	"vulnerabilityThreatIntelEnabled": {
		requires: "ARCANE_FEATURE_VULNERABILITY_MANAGEMENT_ENABLED=true to have effect at runtime.",
	},
	"autoHealInterval": {
		requires: "ARCANE_AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
	"autoHealExcludedContainers": {
		requires: "ARCANE_AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
	"autoHealMaxRestarts": {
		requires: "ARCANE_AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
	"autoHealRestartWindow": {
		requires: "ARCANE_AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
}

// GenerateWithSourceRoot builds the canonical schema document for config and settings docs.
func GenerateWithSourceRoot(sourceRoot string) (*SchemaDocument, error) {
	envConfig, err := collectEnvConfig(sourceRoot)
	if err != nil {
		return nil, fmt.Errorf("collect env config: %w", err)
	}

	return &SchemaDocument{
		SchemaVersion: schemaVersion,
		EnvConfig:     envConfig,
		Settings:      collectSettings(),
		RenamedEnv:    RenamedEnv(),
		RetiredEnv:    slices.Sorted(slices.Values(RetiredEnv)),
	}, nil
}

// MarshalJSON returns a stable, indented JSON encoding of the schema document.
func MarshalJSON(doc *SchemaDocument) ([]byte, error) {
	output, err := json.Marshal(doc, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("marshal schema document: %w", err)
	}

	output = append(output, '\n')
	return output, nil
}

func collectEnvConfig(sourceRoot string) ([]ConfigEntry, error) {
	root, err := resolveSourceRoot(sourceRoot)
	if err != nil {
		return nil, err
	}

	configEntries, err := parseStructEnvFields(
		filepath.Join(root, "internal", "config", "config.go"),
		"Config",
		envFieldOptions{
			sourceFile: sourceFileConfig,
			sourceType: sourceSymbolConfig,
		},
	)
	if err != nil {
		return nil, err
	}

	buildablesEntries, err := parseStructEnvFields(
		filepath.Join(root, "internal", "config", "buildables_config.go"),
		"BuildablesConfig",
		envFieldOptions{
			conditional: true,
			buildTags:   []string{"buildables"},
			sourceFile:  sourceFileBuildablesConfig,
			sourceType:  sourceSymbolBuildablesConfig,
		},
	)
	if err != nil {
		return nil, err
	}

	configEntries = append(configEntries, buildablesEntries...)
	sort.Slice(configEntries, func(i, j int) bool {
		return configEntries[i].Env < configEntries[j].Env
	})

	return configEntries, nil
}

func parseStructEnvFields(filename, structName string, opts envFieldOptions) ([]ConfigEntry, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	structType, err := findStructType(file, structName)
	if err != nil {
		return nil, err
	}

	entries := make([]ConfigEntry, 0, len(structType.Fields.List))
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 || field.Tag == nil {
			continue
		}

		tagValue, unquoteErr := strconv.Unquote(field.Tag.Value)
		if unquoteErr != nil {
			return nil, fmt.Errorf("unquote struct tag for %s: %w", structName, unquoteErr)
		}

		structTag := reflect.StructTag(tagValue)
		envName := structTag.Get("env")
		if envName == "" {
			continue
		}

		options := kit.TrimNonEmpty(strings.Split(structTag.Get("options"), ","))
		typeName, unquoteErr := exprString(field.Type)
		if unquoteErr != nil {
			return nil, fmt.Errorf("render type for %s.%s: %w", structName, field.Names[0].Name, unquoteErr)
		}

		description := cmp.Or(strings.Join(strings.Fields(field.Doc.Text()), " "), strings.Join(strings.Fields(field.Comment.Text()), " "))

		for _, name := range field.Names {
			entries = append(entries, ConfigEntry{
				Env:          envName,
				Field:        name.Name,
				Type:         typeName,
				DefaultValue: structTag.Get("default"),
				Description:  description,
				Options:      options,
				SupportsFile: slices.Contains(options, "file"),
				Conditional:  opts.conditional,
				BuildTags:    slices.Clone(opts.buildTags),
				Source:       opts.sourceType,
				SourceFile:   opts.sourceFile,
				SourceSymbol: opts.sourceType + "." + name.Name,
			})
		}
	}

	return entries, nil
}

func collectSettings() []SettingEntry {
	settingsType := reflect.TypeFor[settings.Settings]()
	entries := make([]SettingEntry, 0, settingsType.NumField())

	for field := range settingsType.Fields() {
		key, attrTag, _ := strings.Cut(field.Tag.Get("key"), ",")
		attrs := kit.TrimNonEmpty(strings.Split(attrTag, ","))
		if key == "" || slices.Contains(attrs, "internal") {
			continue
		}

		meta := utils.ParseMetaTag(field.Tag.Get("meta"))
		minValue, minErr := strconv.ParseFloat(meta["min"], 64)
		maxValue, maxErr := strconv.ParseFloat(meta["max"], 64)
		isSensitive := slices.Contains(attrs, "sensitive")

		entries = append(entries, SettingEntry{
			Env:          settings.EnvName(key, field.Tag.Get("env")),
			Key:          key,
			Type:         cmp.Or(strings.TrimSpace(meta["type"]), "text"),
			Default:      kit.Ternary(isSensitive, "", field.Tag.Get("default")),
			Values:       kit.TrimNonEmpty(strings.Split(meta["values"], "|")),
			Min:          kit.Ternary(minErr == nil, new(minValue), nil),
			Max:          kit.Ternary(maxErr == nil, new(maxValue), nil),
			Scope:        kit.Ternary(slices.Contains(attrs, "manager"), "manager", "environment"),
			Sensitive:    isSensitive,
			Public:       slices.Contains(attrs, "public"),
			Description:  meta["description"],
			Category:     meta["category"],
			Requires:     overrideDocRules[key].requires,
			Note:         overrideDocRules[key].note,
			Source:       "settings.Settings",
			SourceFile:   sourceFileSettings,
			SourceSymbol: "settings.Settings." + field.Name,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category == entries[j].Category {
			return entries[i].Env < entries[j].Env
		}
		return entries[i].Category < entries[j].Category
	})

	return entries
}

func findStructType(file *ast.File, structName string) (*ast.StructType, error) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, localOk := spec.(*ast.TypeSpec)
			if !localOk || typeSpec.Name.Name != structName {
				continue
			}

			structType, localOk := typeSpec.Type.(*ast.StructType)
			if !localOk {
				return nil, fmt.Errorf("%s is not a struct", structName)
			}

			return structType, nil
		}
	}

	return nil, fmt.Errorf("struct %s not found", structName)
}

func exprString(expr ast.Expr) (string, error) {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), expr); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func resolveSourceRoot(sourceRoot string) (string, error) {
	candidates := make([]string, 0, 2)
	if strings.TrimSpace(sourceRoot) != "" {
		candidates = append(candidates, sourceRoot)
	} else {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
		candidates = append(candidates, wd)
	}

	for _, candidate := range candidates {
		resolved, err := resolveSourceRootCandidate(candidate)
		if err == nil {
			return resolved, nil
		}
	}

	if strings.TrimSpace(sourceRoot) != "" {
		return "", fmt.Errorf("resolve source root from %q: expected backend/internal/config/config.go", sourceRoot)
	}

	return "", errors.New("resolve source root: run from the repository root/backend directory or pass --source-root")
}

func resolveSourceRootCandidate(candidate string) (string, error) {
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("abs path for %q: %w", candidate, err)
	}

	for current := candidate; ; current = filepath.Dir(current) {
		if hasSchemaSourceFiles(current) {
			return current, nil
		}

		backendRoot := filepath.Join(current, "backend")
		if hasSchemaSourceFiles(backendRoot) {
			return backendRoot, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}

	return "", fmt.Errorf("schema sources not found from %q", candidate)
}

func hasSchemaSourceFiles(root string) bool {
	required := []string{
		filepath.Join(root, "internal", "config", "config.go"),
		filepath.Join(root, "internal", "config", "buildables_config.go"),
	}

	// os.* rather than acfs: this probes repo source files while walking up
	// arbitrary ancestor directories at codegen time, so no confinement root exists.
	for _, filename := range required {
		if _, err := os.Stat(filename); err != nil {
			return false
		}
	}

	return true
}
