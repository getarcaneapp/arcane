package settings

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"uuid"

	"gorm.io/gorm"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	Env map[string]string
)

func EnsureInstanceID(ctx context.Context, db *database.DB) (string, error) {
	return ensureSettingValue(ctx, db, "instanceId", "instance ID", func() (string, error) { return uuid.New().String(), nil })
}

func ResolveEncryptionKey(ctx context.Context, db *database.DB, cfg *config.Config) (string, error) {
	if !cfg.AgentEnabled && cfg.Environment.IsProdEnvironment() {
		return cfg.EncryptionKey, nil
	}
	var stored SettingVariable
	if err := db.WithContext(ctx).Where("key = ?", "encryptionKey").Limit(1).Find(&stored).Error; err != nil {
		return "", fmt.Errorf("failed to load encryption key: %w", err)
	}
	switch {
	case stored.Value == "" && cfg.EncryptionKey == "":
		return ensureSettingValue(ctx, db, "encryptionKey", "encryption key", func() (string, error) {
			sum := sha256.Sum256([]byte(uuid.New().String()))
			return base64.StdEncoding.EncodeToString(sum[:]), nil
		})
	case stored.Value != "" && cfg.EncryptionKey != "" && stored.Value != cfg.EncryptionKey:
		slog.WarnContext(ctx, "Ignoring ARCANE_ENCRYPTION_KEY because existing secrets are encrypted with the stored encryption key")
	}
	return cmp.Or(stored.Value, cfg.EncryptionKey), nil
}

func EnvNames() []string {
	fields, _ := settingsFields()
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if !field.isInternal {
			names = append(names, field.envName)
		}
	}
	return names
}

func ensureSettingValue(ctx context.Context, db *database.DB, key, label string, generate func() (string, error)) (string, error) {
	var value string
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var setting SettingVariable
		if err := tx.Where("key = ?", key).Limit(1).Find(&setting).Error; err != nil {
			return fmt.Errorf("failed to load %s: %w", label, err)
		}
		if setting.Value != "" {
			value = setting.Value
			return nil
		}

		generated, err := generate()
		if err != nil {
			return err
		}
		if saveErr := tx.Save(&SettingVariable{Key: key, Value: generated}).Error; saveErr != nil {
			return fmt.Errorf("failed to persist %s: %w", label, saveErr)
		}
		value = generated
		return nil
	})
	return value, err
}

func EnvName(key, envTag string) string {
	return cmp.Or(envTag, "ARCANE_"+strings.ToUpper(kit.SnakeCase(key)))
}

func LoadEnv() (Env, error) {
	fields, _ := settingsFields()
	env := Env{}
	var errs []error
	for _, field := range fields {
		if field.isInternal {
			continue
		}
		raw, _, _ := kit.LookupEnvOrFile(field.envName)
		value := kit.TrimQuotes(raw)
		if value == "" {
			continue
		}
		if err := validateValue(field, value); err != nil {
			errs = append(errs, fmt.Errorf("invalid environment variable %s: %w", field.envName, err))
			continue
		}
		env[field.key] = value
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return env, nil
}

func (e Env) Variables() map[string]string {
	_, byKey := settingsFields()
	variables := make(map[string]string, len(e))
	for key, value := range e {
		variables[byKey[key].envName] = value
	}
	return variables
}

func (e Env) ValueOrDefault(key string) string {
	_, byKey := settingsFields()
	return cmp.Or(e[key], byKey[key].defaultValue)
}

func projectsDirectoryContainerPaths(value string) ([]string, error) {
	if !strings.HasPrefix(strings.TrimSpace(value), "[") {
		return nil, errors.New("projectsDirectories must be a JSON array of {\"path\", \"enabled\"} entries")
	}
	var entries []types.ProjectsDirectoryEntry
	if err := json.Unmarshal([]byte(value), &entries); err != nil {
		return nil, fmt.Errorf("projectsDirectories must be a JSON array of {\"path\", \"enabled\"} entries: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		raw := strings.TrimSpace(entry.Path)
		containerPath := kit.Ternary(projects.IsWindowsDrivePath(raw), raw, strings.SplitN(raw, ":", 2)[0])
		if raw == "" || (!projects.IsWindowsDrivePath(raw) && !strings.HasPrefix(containerPath, "/")) {
			return nil, fmt.Errorf("projectsDirectories entry %q must be an absolute path starting with '/'", entry.Path)
		}
		cleaned := filepath.Clean(containerPath)
		if slices.Contains(paths, cleaned) {
			return nil, fmt.Errorf("projectsDirectories contains %q more than once", cleaned)
		}
		for _, existing := range paths {
			if projects.IsSafeSubdirectory(existing, cleaned) || projects.IsSafeSubdirectory(cleaned, existing) {
				return nil, fmt.Errorf("projectsDirectories entries %q and %q overlap; one is inside the other", existing, cleaned)
			}
		}
		paths = append(paths, cleaned)
	}
	return paths, nil
}

func resolveSymlinks(target string) (string, error) {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	existing := absolute
	for {
		resolved, evalErr := filepath.EvalSymlinks(existing)
		if evalErr == nil {
			return filepath.Join(resolved, strings.TrimPrefix(absolute, existing)), nil
		}
		if !errors.Is(evalErr, fs.ErrNotExist) || filepath.Dir(existing) == existing {
			return "", evalErr
		}
		existing = filepath.Dir(existing)
	}
}

func validateProjectsDirectoriesOverlap(ctx context.Context, extrasValue, primaryValue, templatesValue string) error {
	extras, err := projectsDirectoryContainerPaths(extrasValue)
	if err != nil {
		return err
	}
	for _, reserved := range []struct {
		name string
		path string
	}{
		{name: "projects directory", path: projects.ResolveConfiguredContainerDirectory(ctx, primaryValue, "/app/data/projects")},
		{name: "templates directory", path: projects.ResolveConfiguredContainerDirectory(ctx, templatesValue, "/app/data/templates")},
	} {
		for _, extra := range extras {
			if projects.IsSafeSubdirectory(reserved.path, extra) || projects.IsSafeSubdirectory(extra, reserved.path) {
				return fmt.Errorf("projectsDirectories entry %q overlaps the %s %q", extra, reserved.name, reserved.path)
			}
		}
	}
	return nil
}
