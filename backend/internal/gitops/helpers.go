package gitops

import (
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

func validateDeploySource(sync project.GitOpsSync) error {
	invalid := func(field, message string) error {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: field, Err: errors.New(message)})
	}
	if composePath := path.Clean(filepath.ToSlash(strings.TrimSpace(sync.ComposePath))); composePath == ".." || strings.HasPrefix(composePath, "../") {
		return invalid("composePath", "Compose path must stay inside the repository.")
	}
	overridePath := strings.TrimSpace(sync.ComposeOverridePath)
	cleanedOverride := path.Clean(filepath.ToSlash(overridePath))
	switch {
	case overridePath == "":
	case len(overridePath) > 512:
		return invalid("composeOverridePath", "Override path must be 512 characters or fewer.")
	case path.IsAbs(cleanedOverride) || cleanedOverride == ".." || strings.HasPrefix(cleanedOverride, "../"):
		return invalid("composeOverridePath", "Override path must be relative to the repository root.")
	case !slices.Contains([]string{".yml", ".yaml"}, strings.ToLower(path.Ext(cleanedOverride))):
		return invalid("composeOverridePath", "Override file must be a .yml or .yaml file.")
	case cleanedOverride == path.Clean(filepath.ToSlash(sync.ComposePath)):
		return invalid("composeOverridePath", "Override file must differ from the compose file.")
	case sync.TargetType != "swarm_stack" && sync.SyncDirectory:
		return invalid("composeOverridePath", "Project directory syncs only merge compose.override.yaml beside the compose file; rename it or list it in COMPOSE_FILE.")
	}
	if _, err := projects.ParseProjectEnvContent(kit.FromPtr(sync.EnvContent), nil); err != nil {
		return invalid("envContent", err.Error())
	}
	return nil
}

func normalizeSyncLimitSetting(value, defaultValue int) int {
	return kit.Ternary(value < 0, defaultValue, value)
}

func nullableTrimmedString(p *string) *string {
	trimmed := strings.TrimSpace(kit.FromPtr(p))
	return kit.Ternary(trimmed == "", nil, &trimmed)
}

func validateSyncLimits(maxFiles *int, maxTotalSize, maxBinarySize *int64) error {
	if kit.FromPtr(maxFiles) < 0 {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: "maxSyncFiles", Err: errors.New("maxSyncFiles must be non-negative")})
	}
	if kit.FromPtr(maxTotalSize) < 0 {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: "maxSyncTotalSize", Err: errors.New("maxSyncTotalSize must be non-negative")})
	}
	if kit.FromPtr(maxBinarySize) < 0 {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: "maxSyncBinarySize", Err: errors.New("maxSyncBinarySize must be non-negative")})
	}
	return nil
}

func hasPreDeployConfig(submitted map[string]bool) bool {
	return slices.ContainsFunc([]string{
		"preDeployScriptPath",
		"preDeployRunnerImage",
		"preDeployEnv",
		"preDeployExtraMounts",
		"preDeployTimeoutSec",
		"preDeployNetworkMode",
	}, func(field string) bool {
		return submitted[field]
	})
}
