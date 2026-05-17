package database

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/golang-migrate/migrate/v4/database"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEmbeddedMigrationVersions_ProvidersMatch(t *testing.T) {
	sqliteVersions, err := getEmbeddedMigrationVersionsInternal("sqlite")
	require.NoError(t, err)

	postgresVersions, err := getEmbeddedMigrationVersionsInternal("postgres")
	require.NoError(t, err)

	assert.Equal(t, sqliteVersions, postgresVersions)
	require.NotEmpty(t, sqliteVersions)

	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	assert.Equal(t, sqliteVersions[len(sqliteVersions)-1], highest)
}

func TestMigrateDatabase_BlocksStartupDowngrade(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabaseUpInternal(driver, "sqlite"))
	targetVersion := downgradeTargetVersionInternal(t)

	err := migrateDatabaseUpToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", targetVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "newer than this Arcane version supports")
}

func TestMigrateDatabase_DowngradesToExplicitVersion(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabaseUpInternal(driver, "sqlite"))
	targetVersion := downgradeTargetVersionInternal(t)

	require.NoError(t, migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", targetVersion))

	instance, checkSourceDriver, err := newEmbeddedMigrateInstanceInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite")
	require.NoError(t, err)
	defer closeMigrateSourceInternal(checkSourceDriver, "test embedded migrate source")
	currentVersion, currentDirty, versionErr := instance.Version()
	require.NoError(t, versionErr)
	assert.Equal(t, targetVersion, currentVersion)
	assert.False(t, currentDirty)
}

func TestMigrateDatabase_DowngradeRejectsDirtyState(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabaseUpInternal(driver, "sqlite"))
	targetVersion := downgradeTargetVersionInternal(t)
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	highestVersionInt, err := safeUintToIntInternal(highestVersion)
	require.NoError(t, err)
	require.NoError(t, newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db").SetVersion(highestVersionInt, true))

	err = migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", targetVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "dirty")
}

func TestMigrateDatabase_DirtyCurrentVersionRequiresResolution(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabaseUpInternal(driver, "sqlite"))
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	highestVersionInt, err := safeUintToIntInternal(highestVersion)
	require.NoError(t, err)
	require.NoError(t, newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db").SetVersion(highestVersionInt, true))

	err = migrateDatabaseUpToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", highestVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "is dirty")
	assert.ErrorContains(t, err, "resolve the failed migration")
}

func TestMigrateDatabase_DirtyOlderVersionRequiresResolution(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabaseUpInternal(driver, "sqlite"))
	targetVersion := downgradeTargetVersionInternal(t)
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)

	require.NoError(t, migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", targetVersion))

	targetVersionInt, err := safeUintToIntInternal(targetVersion)
	require.NoError(t, err)
	require.NoError(t, newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db").SetVersion(targetVersionInt, true))

	err = migrateDatabaseUpToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", highestVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "dirty")
	assert.ErrorContains(t, err, "resolve the failed migration")
}

func downgradeTargetVersionInternal(t *testing.T) uint {
	t.Helper()

	allVersions, err := getEmbeddedMigrationVersionsInternal("sqlite")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(allVersions), 2, "need at least 2 migration versions to test downgrade")

	return allVersions[len(allVersions)-2]
}

func newSQLiteMigrationDriverInternal(t *testing.T, dirPath, fileName string) database.Driver {
	t.Helper()

	dsn := "file:" + filepath.Join(dirPath, fileName)
	db, err := gorm.Open(glsqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	driver, err := sqliteMigrate.WithInstance(sqlDB, &sqliteMigrate.Config{})
	require.NoError(t, err)

	return driver
}

func TestInitialize_RequiresMigratedSchema(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-init.db")

	db, err := Initialize(ctx, dsn)
	require.Error(t, err)
	assert.Nil(t, db)
	assert.ErrorContains(t, err, "run arcane migrate up")
}

func TestInitialize_AllowsMigratedSchema(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-init.db")

	require.NoError(t, MigrateUp(ctx, dsn))

	db, err := Initialize(ctx, dsn)
	require.NoError(t, err)
	require.NotNil(t, db)

	var settingsCount int64
	require.NoError(t, db.WithContext(ctx).Table("settings").Count(&settingsCount).Error)

	require.NoError(t, db.Close())
}

func TestInitialize_CreatesQueryPerformanceIndexes(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-indexes.db")

	require.NoError(t, MigrateUp(ctx, dsn))

	db, err := Initialize(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	indexes := []string{
		"idx_environments_access_token_not_null",
		"idx_environments_enabled_true",
		"idx_api_keys_expires_at_not_null",
		"idx_api_keys_user_managed_by_created_at",
		"idx_git_repositories_enabled_url",
		"idx_git_repositories_auth_type",
		"idx_gitops_syncs_environment_auto_sync",
		"idx_gitops_syncs_auto_sync_true",
		"idx_gitops_syncs_environment_last_sync_status",
		"idx_gitops_syncs_environment_repository_id",
		"idx_gitops_syncs_environment_project_id",
		"idx_projects_path_unique",
		"idx_projects_dir_name_not_null",
		"idx_compose_templates_lookup_name",
		"idx_compose_templates_lookup_description",
		"idx_volume_backups_volume_name_created_at",
		"idx_image_builds_environment_created_at",
		"idx_image_builds_environment_status",
		"idx_events_environment_timestamp",
		"idx_image_updates_repository_tag",
		"idx_vulnerability_scans_status_total_count",
		"idx_vulnerability_ignores_env_created_at",
		"idx_vulnerability_ignores_env_vulnerability_id",
	}

	for _, indexName := range indexes {
		assertSQLiteIndexExistsInternal(t, db, indexName)
	}

	removedIndexes := []string{
		"idx_api_keys_user_id",
		"idx_events_environment_id",
		"idx_image_update_repository",
		"idx_image_update_tag",
		"idx_volume_backups_volume_name",
		"idx_vulnerability_ignores_env",
		"idx_vulnerability_ignores_vuln",
		"idx_vulnerability_scans_status",
	}

	for _, indexName := range removedIndexes {
		assertSQLiteIndexMissingInternal(t, db, indexName)
	}
}

func TestResolveAppMigrationVersion(t *testing.T) {
	version, err := ResolveAppMigrationVersion("v1.18.0")
	require.NoError(t, err)
	assert.Equal(t, uint(47), version)

	version, err = ResolveAppMigrationVersion("1.19.2")
	require.NoError(t, err)
	assert.Equal(t, uint(52), version)
}

func TestResolveAppMigrationVersion_Unknown(t *testing.T) {
	_, err := ResolveAppMigrationVersion("v0.0.0")
	require.Error(t, err)
	assert.ErrorContains(t, err, "no migration target is known")
}

func TestMigrationVersionManifestIsGenerated(t *testing.T) {
	ctx := context.Background()
	repoRoot := findRepoRootInternal(t)

	generatedVersions, err := GenerateAppMigrationVersionsFromGit(ctx, repoRoot, "")
	require.NoError(t, err)

	currentVersion := currentArcaneVersionInternal(t, repoRoot)
	if !appMigrationVersionsContainInternal(generatedVersions, currentVersion) {
		generatedVersions, err = GenerateAppMigrationVersionsFromGit(ctx, repoRoot, currentVersion)
		require.NoError(t, err)
	}

	committedVersions, err := ListAppMigrationVersions()
	require.NoError(t, err)
	assert.Equal(t, generatedVersions, committedVersions, "run `just migration-manifest` to regenerate backend/resources/migration_versions.json")
}

func TestGetMigrationStatus(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-status.db")
	require.NoError(t, MigrateUp(ctx, dsn))

	status, err := GetMigrationStatus(ctx, dsn)
	require.NoError(t, err)

	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	assert.Equal(t, "sqlite", status.Provider)
	assert.True(t, status.HasVersion)
	assert.False(t, status.Dirty)
	assert.Equal(t, highest, status.CurrentVersion)
	assert.Equal(t, highest, status.LatestVersion)
}

func assertSQLiteIndexExistsInternal(t *testing.T, db *DB, indexName string) {
	t.Helper()

	var result struct {
		Name string
	}

	err := db.Raw(
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?",
		indexName,
	).Scan(&result).Error
	require.NoError(t, err)
	assert.Equal(t, indexName, result.Name)
}

func assertSQLiteIndexMissingInternal(t *testing.T, db *DB, indexName string) {
	t.Helper()

	var count int64

	err := db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?",
		indexName,
	).Scan(&count).Error
	require.NoError(t, err)
	assert.Zero(t, count, "expected index %s to be removed", indexName)
}

func findRepoRootInternal(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err)

	for {
		if fileExistsInternal(filepath.Join(wd, ".git")) && fileExistsInternal(filepath.Join(wd, ".arcane.json")) {
			return wd
		}

		parent := filepath.Dir(wd)
		if parent == wd {
			t.Skip("repository root with .git was not found")
		}
		wd = parent
	}
}

func currentArcaneVersionInternal(t *testing.T, repoRoot string) string {
	t.Helper()

	configBytes, err := os.ReadFile(filepath.Join(repoRoot, ".arcane.json"))
	require.NoError(t, err)

	var arcaneConfig struct {
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(configBytes, &arcaneConfig))
	require.NotEmpty(t, arcaneConfig.Version)
	return arcaneConfig.Version
}

func appMigrationVersionsContainInternal(versions []AppMigrationVersion, appVersion string) bool {
	normalizedVersion := normalizeAppVersionInternal(appVersion)
	for _, version := range versions {
		if normalizeAppVersionInternal(version.AppVersion) == normalizedVersion {
			return true
		}
	}
	return false
}

func fileExistsInternal(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
