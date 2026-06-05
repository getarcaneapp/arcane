package database

import (
	"context"
	stdsql "database/sql"
	"path/filepath"
	"strconv"
	"testing"

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

func TestEnsureSQLiteDirectoryPreservesAbsoluteFilePath(t *testing.T) {
	tempDir := t.TempDir()
	dsn := "file:" + filepath.Join(tempDir, "nested", "arcane-test.db")

	require.NoError(t, ensureSQLiteDirectoryInternal(dsn))
	require.DirExists(t, filepath.Join(tempDir, "nested"))
	require.NoDirExists(t, filepath.Join("var", "folders"))
}

func TestMigrateDatabase_BlocksDowngradeWithoutFlag(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-test.db")
	require.NoError(t, migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}))
	targetVersion := downgradeTargetVersionInternal(t)

	err := migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, targetVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")
	assert.ErrorContains(t, err, "newer than this Arcane binary supports")

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersionInternal(t, dsn))
}

func TestMigrateDatabase_DowngradesWhenAllowed(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-test.db")
	require.NoError(t, migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}))
	targetVersion := downgradeTargetVersionInternal(t)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, targetVersion))
	assert.Equal(t, targetVersion, readGooseSQLiteVersionInternal(t, dsn))
}

func TestMigrateDatabase_BlocksFutureGooseVersionWithoutFlag(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-future.db")
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	require.NoError(t, createGooseVersionTableInternal(ctx, rawDB, dbProviderSQLite))
	require.NoError(t, insertGooseMigrationVersionInternal(ctx, rawDB, dbProviderSQLite, 0))
	require.NoError(t, insertGooseMigrationVersionInternal(ctx, rawDB, dbProviderSQLite, highestVersion+1))

	err = migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "newer than this Arcane binary supports")
	assert.Equal(t, highestVersion+1, readGooseSQLiteVersionInternal(t, dsn))
}

func TestMigrateDatabase_BlocksDowngradeWhenEmbeddedMigrationMissing(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-missing-down.db")
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	require.NoError(t, createGooseVersionTableInternal(ctx, rawDB, dbProviderSQLite))
	require.NoError(t, insertGooseMigrationVersionInternal(ctx, rawDB, dbProviderSQLite, 0))
	require.NoError(t, insertGooseMigrationVersionInternal(ctx, rawDB, dbProviderSQLite, highestVersion+1))

	err = migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true})
	require.Error(t, err)
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true is not sufficient")
	assert.ErrorContains(t, err, "restore the database from a backup")
	assert.ErrorContains(t, err, strconv.FormatInt(highestVersion+1, 10))
	assert.Equal(t, highestVersion+1, readGooseSQLiteVersionInternal(t, dsn))
}

func TestMigrateDatabase_BlocksDirtyLegacyCurrentVersion(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-legacy-current-dirty.db")
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	seedLegacyMigrationStateInternal(t, dsn, highestVersion, true)

	err = migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "is dirty")
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")

	require.NoError(t, migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}))
	assert.Equal(t, highestVersion, readGooseSQLiteVersionInternal(t, dsn))
	assertLegacyMigrationDirtyInternal(t, dsn, false)
}

func TestMigrateDatabase_BlocksDirtyLegacyOlderVersion(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-legacy-older-dirty.db")
	targetVersion := downgradeTargetVersionInternal(t)
	require.NoError(t, migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}))
	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, targetVersion))
	require.NoError(t, clearGooseVersionTableInternal(ctx, rawDB, dbProviderSQLite))
	seedLegacyMigrationStateInternal(t, dsn, targetVersion, true)

	err := migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "is dirty")
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")

	require.NoError(t, migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}))
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersionInternal(t, dsn))
	assertLegacyMigrationDirtyInternal(t, dsn, false)
}

func downgradeTargetVersionInternal(t *testing.T) int64 {
	t.Helper()

	allVersions, err := getEmbeddedMigrationVersionsInternal("sqlite")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(allVersions), 2, "need at least 2 migration versions to test downgrade")

	return allVersions[len(allVersions)-2]
}

func newSQLiteSQLDBInternal(t *testing.T, dirPath, fileName string) (*stdsql.DB, string) {
	t.Helper()

	dsn := "file:" + filepath.Join(dirPath, fileName)
	db, err := stdsql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db, dsn
}

func TestInitialize_AllowsMigrationOptions(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-init.db")

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	require.NotNil(t, db)

	var settingsCount int64
	require.NoError(t, db.WithContext(ctx).Table("settings").Count(&settingsCount).Error)

	require.NoError(t, db.Close())
}

func TestInitialize_RecordsGooseVersionOnFreshSQLite(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-goose-fresh.db")

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	assert.Equal(t, highest, readGooseSQLiteVersionInternal(t, dsn))
}

func TestInitialize_AdoptsCleanLegacyMigrationState(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-legacy-clean.db")
	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	seedLegacyMigrationStateInternal(t, dsn, highest, false)

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	assert.Equal(t, highest, readGooseSQLiteVersionInternal(t, dsn))
	assertLegacyMigrationDirtyInternal(t, dsn, false)
}

func TestInitialize_RollsBackFailedLegacyMigrationAdoption(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-legacy-rollback.db")
	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	seedLegacyMigrationStateInternal(t, dsn, highest, false)

	_, err = rawDB.Exec(`
CREATE TABLE goose_db_version (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	version_id INTEGER NOT NULL,
	is_applied INTEGER NOT NULL CHECK (is_applied = 0),
	tstamp TIMESTAMP DEFAULT (datetime('now'))
)`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, ?)`, 0, 0)
	require.NoError(t, err)

	err = adoptLegacyMigrationStateInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to insert Goose migration version")

	var rowCount int
	err = rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 0 AND is_applied = 0`).Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}

func TestInitialize_BlocksDirtyLegacyMigrationState(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-legacy-dirty.db")
	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	seedLegacyMigrationStateInternal(t, dsn, highest, true)

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.Error(t, err)
	require.Nil(t, db)
	assert.ErrorContains(t, err, "dirty")
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")
}

func TestInitialize_ClearsDirtyLegacyMigrationStateWhenAllowed(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-legacy-dirty-allowed.db")
	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	seedLegacyMigrationStateInternal(t, dsn, highest, true)

	db, err := Initialize(ctx, dsn, MigrationOptions{AllowDowngrade: true})
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	assert.Equal(t, highest, readGooseSQLiteVersionInternal(t, dsn))
	assertLegacyMigrationDirtyInternal(t, dsn, false)
}

func TestInitialize_CreatesQueryPerformanceIndexes(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-indexes.db")

	db, err := Initialize(ctx, dsn, MigrationOptions{})
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

func seedLegacyMigrationStateInternal(t *testing.T, dsn string, version int64, dirty bool) {
	t.Helper()

	rawDB, err := stdsql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rawDB.Close())
	})

	_, err = rawDB.Exec(`
CREATE TABLE schema_migrations (
	version INTEGER NOT NULL PRIMARY KEY,
	dirty BOOLEAN NOT NULL
)`)
	require.NoError(t, err)

	_, err = rawDB.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (?, ?)`, version, dirty)
	require.NoError(t, err)
}

func readGooseSQLiteVersionInternal(t *testing.T, dsn string) int64 {
	t.Helper()

	rawDB, err := stdsql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rawDB.Close())
	}()

	var version int64
	err = rawDB.QueryRow(`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1`).Scan(&version)
	require.NoError(t, err)
	return version
}

func assertLegacyMigrationDirtyInternal(t *testing.T, dsn string, expected bool) {
	t.Helper()

	rawDB, err := stdsql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rawDB.Close())
	}()

	var dirty bool
	err = rawDB.QueryRow(`SELECT dirty FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&dirty)
	require.NoError(t, err)
	assert.Equal(t, expected, dirty)
}
