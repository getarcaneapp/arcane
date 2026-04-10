package configschema

import (
	"bytes"
	"encoding/json"
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

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
)

const (
	schemaVersion                = 1
	sourceFileConfig             = "backend/internal/config/config.go"
	sourceFileBuildablesConfig   = "backend/internal/config/buildables_config.go"
	sourceFileSettings           = "backend/internal/models/settings.go"
	sourceSymbolConfig           = "config.Config"
	sourceSymbolBuildablesConfig = "config.BuildablesConfig"
)

var documentNotes = []string{
	"When AGENT_MODE=true or UI_CONFIGURATION_DISABLED=true, Arcane manages non-internal settings through environment variables more broadly than the always-on envOverride path.",
	"The settingEnvOverrides section includes stable envOverride settings plus non-internal settings that are env-managed in env-only modes.",
}

// SchemaDocument is the canonical JSON artifact consumed by the docs site.
type SchemaDocument struct {
	SchemaVersion       int                    `json:"schemaVersion"`
	Notes               []string               `json:"notes,omitempty"`
	EnvConfig           []ConfigEntry          `json:"envConfig"`
	SettingEnvOverrides []SettingOverrideEntry `json:"settingEnvOverrides"`
}

// ConfigEntry describes an environment-backed runtime config value.
type ConfigEntry struct {
	Env          string   `json:"env"`
	Field        string   `json:"field"`
	Type         string   `json:"type"`
	DefaultValue string   `json:"defaultValue,omitempty"`
	Description  string   `json:"description,omitempty"`
	Options      []string `json:"options,omitempty"`
	SupportsFile bool     `json:"supportsFile,omitempty"`
	Conditional  bool     `json:"conditional,omitempty"`
	BuildTags    []string `json:"buildTags,omitempty"`
	Source       string   `json:"source"`
	SourceFile   string   `json:"sourceFile"`
	SourceSymbol string   `json:"sourceSymbol"`
}

// SettingOverrideEntry describes a database-backed setting that can be controlled by env vars.
type SettingOverrideEntry struct {
	Env          string `json:"env"`
	SettingKey   string `json:"settingKey"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
	Type         string `json:"type"`
	Sensitive    bool   `json:"sensitive"`
	Deprecated   bool   `json:"deprecated"`
	Category     string `json:"category,omitempty"`
	Public       bool   `json:"public"`
	Requires     string `json:"requires,omitempty"`
	Note         string `json:"note,omitempty"`
	Source       string `json:"source"`
	SourceFile   string `json:"sourceFile"`
	SourceSymbol string `json:"sourceSymbol"`
}

type envFieldOptions struct {
	conditional bool
	buildTags   []string
	sourceFile  string
	sourceType  string
}

type overrideDocRule struct {
	requires   string
	note       string
	deprecated bool
}

var overrideDocRules = map[string]overrideDocRule{
	"authOidcConfig": {
		deprecated: true,
		note:       "Deprecated legacy JSON OIDC configuration. Prefer the discrete OIDC_* environment variables.",
	},
	"autoUpdateInterval": {
		requires: "AUTO_UPDATE=true to have effect at runtime.",
	},
	"scheduledPruneInterval": {
		requires: "SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"scheduledPruneContainers": {
		requires: "SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"scheduledPruneImages": {
		requires: "SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"scheduledPruneVolumes": {
		requires: "SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"scheduledPruneNetworks": {
		requires: "SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"scheduledPruneBuildCache": {
		requires: "SCHEDULED_PRUNE_ENABLED=true to have effect at runtime.",
	},
	"vulnerabilityScanInterval": {
		requires: "VULNERABILITY_SCAN_ENABLED=true to have effect at runtime.",
	},
	"autoHealInterval": {
		requires: "AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
	"autoHealExcludedContainers": {
		requires: "AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
	"autoHealMaxRestarts": {
		requires: "AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
	"autoHealRestartWindow": {
		requires: "AUTO_HEAL_ENABLED=true to have effect at runtime.",
	},
}

// GenerateWithSourceRoot builds the canonical schema document for config and settings docs.
func GenerateWithSourceRoot(sourceRoot string) (*SchemaDocument, error) {
	envConfig, err := collectEnvConfigInternal(sourceRoot)
	if err != nil {
		return nil, fmt.Errorf("collect env config: %w", err)
	}

	settingOverrides, err := collectSettingEnvOverridesInternal()
	if err != nil {
		return nil, fmt.Errorf("collect setting env overrides: %w", err)
	}

	doc := &SchemaDocument{
		SchemaVersion:       schemaVersion,
		Notes:               slices.Clone(documentNotes),
		EnvConfig:           envConfig,
		SettingEnvOverrides: settingOverrides,
	}

	return doc, nil
}

// MarshalJSON returns a stable, indented JSON encoding of the schema document.
func MarshalJSON(doc *SchemaDocument) ([]byte, error) {
	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal schema document: %w", err)
	}

	output = append(output, '\n')
	return output, nil
}

func collectEnvConfigInternal(sourceRoot string) ([]ConfigEntry, error) {
	root, err := resolveSourceRootInternal(sourceRoot)
	if err != nil {
		return nil, err
	}

	configEntries, err := parseStructEnvFieldsInternal(
		filepath.Join(root, "internal/config/config.go"),
		"Config",
		envFieldOptions{
			sourceFile: sourceFileConfig,
			sourceType: sourceSymbolConfig,
		},
	)
	if err != nil {
		return nil, err
	}

	buildablesEntries, err := parseStructEnvFieldsInternal(
		filepath.Join(root, "internal/config/buildables_config.go"),
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

func parseStructEnvFieldsInternal(filename, structName string, opts envFieldOptions) ([]ConfigEntry, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	structType, err := findStructTypeInternal(file, structName)
	if err != nil {
		return nil, err
	}

	entries := make([]ConfigEntry, 0, len(structType.Fields.List))
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 || field.Tag == nil {
			continue
		}

		tagValue, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			return nil, fmt.Errorf("unquote struct tag for %s: %w", structName, err)
		}

		structTag := reflect.StructTag(tagValue)
		envName := structTag.Get("env")
		if envName == "" {
			continue
		}

		options := splitTagListInternal(structTag.Get("options"))
		typeName, err := exprStringInternal(field.Type)
		if err != nil {
			return nil, fmt.Errorf("render type for %s.%s: %w", structName, field.Names[0].Name, err)
		}

		description := normalizeCommentInternal(field.Doc.Text())
		if description == "" {
			description = normalizeCommentInternal(field.Comment.Text())
		}

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

func collectSettingEnvOverridesInternal() ([]SettingOverrideEntry, error) {
	defaults := services.DefaultSettingsConfig()
	settingsType := reflect.TypeFor[models.Settings]()
	entries := make([]SettingOverrideEntry, 0, settingsType.NumField())

	for field := range settingsType.Fields() {
		keyTag := field.Tag.Get("key")
		if keyTag == "" {
			continue
		}

		tagParts := splitTagListInternal(keyTag)
		if len(tagParts) == 0 {
			continue
		}

		key := tagParts[0]
		attrs := tagParts[1:]
		hasEnvOverride := slices.Contains(attrs, "envOverride")
		isInternal := slices.Contains(attrs, "internal")
		if !hasEnvOverride && isInternal {
			continue
		}

		defaultValue, isPublic, isSensitive, err := defaults.FieldByKey(key)
		if err != nil {
			return nil, fmt.Errorf("lookup default value for %q: %w", key, err)
		}
		if isSensitive {
			defaultValue = ""
		}

		meta := utils.ParseMetaTag(field.Tag.Get("meta"))
		rule := overrideDocRules[key]
		requires := rule.requires
		if !hasEnvOverride {
			requires = joinRequirementsInternal(
				"AGENT_MODE=true or UI_CONFIGURATION_DISABLED=true to manage this setting via env.",
				requires,
			)
		}

		entries = append(entries, SettingOverrideEntry{
			Env:          utils.CamelCaseToScreamingSnakeCase(key),
			SettingKey:   key,
			Description:  meta["description"],
			DefaultValue: defaultValue,
			Type:         defaultStringInternal(meta["type"], "text"),
			Sensitive:    isSensitive,
			Deprecated:   rule.deprecated || slices.Contains(attrs, "deprecated"),
			Category:     meta["category"],
			Public:       isPublic,
			Requires:     requires,
			Note:         rule.note,
			Source:       "models.Settings + services.DefaultSettingsConfig",
			SourceFile:   sourceFileSettings,
			SourceSymbol: "models.Settings." + field.Name,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category == entries[j].Category {
			return entries[i].Env < entries[j].Env
		}
		return entries[i].Category < entries[j].Category
	})

	return entries, nil
}

func findStructTypeInternal(file *ast.File, structName string) (*ast.StructType, error) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != structName {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return nil, fmt.Errorf("%s is not a struct", structName)
			}

			return structType, nil
		}
	}

	return nil, fmt.Errorf("struct %s not found", structName)
}

func exprStringInternal(expr ast.Expr) (string, error) {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), expr); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func splitTagListInternal(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}

	return result
}

func normalizeCommentInternal(comment string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(comment), " "))
}

func joinRequirementsInternal(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}

	return strings.Join(filtered, " ")
}

func defaultStringInternal(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func resolveSourceRootInternal(sourceRoot string) (string, error) {
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
		resolved, err := resolveSourceRootCandidateInternal(candidate)
		if err == nil {
			return resolved, nil
		}
	}

	if strings.TrimSpace(sourceRoot) != "" {
		return "", fmt.Errorf("resolve source root from %q: expected backend/internal/config/config.go", sourceRoot)
	}

	return "", fmt.Errorf("resolve source root: run from the repository root/backend directory or pass --source-root")
}

func resolveSourceRootCandidateInternal(candidate string) (string, error) {
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("abs path for %q: %w", candidate, err)
	}

	for current := candidate; ; current = filepath.Dir(current) {
		if hasSchemaSourceFilesInternal(current) {
			return current, nil
		}

		backendRoot := filepath.Join(current, "backend")
		if hasSchemaSourceFilesInternal(backendRoot) {
			return backendRoot, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}

	return "", fmt.Errorf("schema sources not found from %q", candidate)
}

func hasSchemaSourceFilesInternal(root string) bool {
	required := []string{
		filepath.Join(root, "internal/config/config.go"),
		filepath.Join(root, "internal/config/buildables_config.go"),
	}

	for _, filename := range required {
		if _, err := os.Stat(filename); err != nil {
			return false
		}
	}

	return true
}
