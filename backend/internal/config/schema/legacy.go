package schema

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

var (
	RetiredEnv         = []string{"JWT_SECRET", "UPDATE_CHECK_DISABLED", "ANALYTICS_DISABLED"}
	envFileSuffixes    = []string{"", "_FILE", "__FILE"}
	alreadyPrefixedEnv = []string{
		"ARCANE_BACKUP_VOLUME_NAME",
		"ARCANE_IN_CONTAINER",
		"ARCANE_UPGRADE_KEEP_CONTAINER",
		"ARCANE_ANALYTICS_ENABLED",
		"ARCANE_UPDATE_CHECK_ENABLED",
		"ARCANE_AGENT",
	}
)

func RenamedEnv() map[string]string {
	renamed := map[string]string{}
	for _, name := range slices.Concat(config.EnvNames(), settings.EnvNames(), []string{"ARCANE_AUTO_LOGIN_USERNAME", "ARCANE_AUTO_LOGIN_PASSWORD"}) {
		if legacy, ok := strings.CutPrefix(name, "ARCANE_"); ok && !slices.Contains(alreadyPrefixedEnv, name) {
			renamed[legacy] = name
		}
		if suffix, ok := strings.CutPrefix(name, "ARCANE_AGENT"); ok && name != "ARCANE_AGENT_TOKEN" {
			renamed["EDGE"+cmp.Or(suffix, "_AGENT")] = name
			renamed["ARCANE_EDGE"+cmp.Or(suffix, "_AGENT")] = name
		}
	}
	return renamed
}

func ManifestEnvironment(manifest types.RecoveryManifest) map[string]string {
	if manifest.FormatVersion >= types.RecoveryManifestFormatVersion {
		return manifest.Environment
	}
	renamed := RenamedEnv()
	environment := make(map[string]string, len(manifest.Environment))
	for name, value := range manifest.Environment {
		target, legacy := renamed[name]
		if _, exists := manifest.Environment[target]; legacy && !exists {
			name = target
		}
		environment[name] = value
	}
	return environment
}

func CheckLegacyEnv(ctx context.Context) error {
	renamed := RenamedEnv()
	var errs []error
	for _, legacy := range slices.Sorted(maps.Keys(renamed)) {
		if slices.ContainsFunc(envFileSuffixes, func(suffix string) bool {
			return os.Getenv(renamed[legacy]+suffix) != ""
		}) {
			continue
		}
		for _, suffix := range envFileSuffixes {
			if os.Getenv(legacy+suffix) != "" {
				errs = append(errs, fmt.Errorf("rename %s to %s", legacy+suffix, renamed[legacy]+suffix))
			}
		}
	}
	for _, retired := range RetiredEnv {
		for _, suffix := range envFileSuffixes {
			if os.Getenv(retired+suffix) != "" {
				slog.WarnContext(ctx, "Retired environment variable is set and no longer used; remove it from your environment", "env", retired+suffix)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("legacy environment variables are no longer read:\n%w", err)
	}
	return nil
}
