package database

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/resources"
)

type AppMigrationVersion struct {
	AppVersion       string `json:"appVersion"`
	MigrationVersion uint   `json:"migrationVersion"`
}

type migrationVersionManifest struct {
	Versions []AppMigrationVersion `json:"versions"`
}

func ResolveAppMigrationVersion(appVersion string) (uint, error) {
	normalizedVersion := normalizeAppVersionInternal(appVersion)
	if normalizedVersion == "" {
		return 0, fmt.Errorf("target app version is required")
	}

	versions, err := ListAppMigrationVersions()
	if err != nil {
		return 0, err
	}

	for _, version := range versions {
		if normalizeAppVersionInternal(version.AppVersion) == normalizedVersion {
			return version.MigrationVersion, nil
		}
	}

	return 0, fmt.Errorf("no migration target is known for Arcane version %q", appVersion)
}

func ListAppMigrationVersions() ([]AppMigrationVersion, error) {
	manifestBytes, err := resources.FS.ReadFile("migration_versions.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read migration version manifest: %w", err)
	}

	var manifest migrationVersionManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse migration version manifest: %w", err)
	}
	if len(manifest.Versions) == 0 {
		return nil, fmt.Errorf("migration version manifest has no versions")
	}

	versions := make([]AppMigrationVersion, len(manifest.Versions))
	copy(versions, manifest.Versions)
	return versions, nil
}

func normalizeAppVersionInternal(appVersion string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(appVersion), "v"), "V")
}
