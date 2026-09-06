package database

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sqliteutil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/sqlite"

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
	require.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")
	require.ErrorContains(t, err, "newer than this Arcane binary supports")

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

func TestMigration065_ProjectBuildImageRefs_UpAndDown(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-project-build-refs.db")

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 64))
	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name = 'build_image_refs_json'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 65))
	var notNull int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*), COALESCE(MAX("notnull"), 0) FROM pragma_table_info('projects') WHERE name = 'build_image_refs_json'`).Scan(&columnCount, &notNull))
	assert.Equal(t, 1, columnCount)
	assert.Zero(t, notNull)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 64))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name = 'build_image_refs_json'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
}

func TestMigration066_GlobalVariables_UpAndDown(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-global-variables.db")

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 65))
	var tableCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('global_variables', 'global_variable_environments')`).Scan(&tableCount))
	assert.Zero(t, tableCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 66))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('global_variables', 'global_variable_environments')`).Scan(&tableCount))
	assert.Equal(t, 2, tableCount)

	_, err := rawDB.Exec(`INSERT INTO environments (id, api_url, status, enabled) VALUES ('env-1', 'http://localhost', 'online', TRUE)`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO global_variables (id, created_at, key, value, is_secret, all_environments) VALUES ('var-1', CURRENT_TIMESTAMP, 'API_URL', 'https://example.test', FALSE, FALSE)`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO global_variable_environments (global_variable_id, environment_id) VALUES ('var-1', 'env-1')`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 65))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('global_variables', 'global_variable_environments')`).Scan(&tableCount))
	assert.Zero(t, tableCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 66))
	var rowCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM global_variables`).Scan(&rowCount))
	assert.Zero(t, rowCount)
}

func TestMigration067_ActivityBatchID_UpAndDown(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-activity-batch-id.db")

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 66))
	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('activities') WHERE name = 'batch_id'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 67))
	var notNull int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*), COALESCE(MAX("notnull"), 0) FROM pragma_table_info('activities') WHERE name = 'batch_id'`).Scan(&columnCount, &notNull))
	assert.Equal(t, 1, columnCount)
	assert.Zero(t, notNull)

	var indexCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_activities_environment_batch'`).Scan(&indexCount))
	assert.Equal(t, 1, indexCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 66))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('activities') WHERE name = 'batch_id'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_activities_environment_batch'`).Scan(&indexCount))
	assert.Zero(t, indexCount)
}

func TestMigration068_UserPreferences_UpAndDown(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-user-preferences.db")

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 67))
	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'preferences'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	_, err := rawDB.Exec(`INSERT INTO users (id, created_at, updated_at, username, password_hash) VALUES ('u-1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'kyle', 'hash')`)
	require.NoError(t, err)
	// One string setting and one boolean setting are seeded; iconCatalog is
	// deliberately absent so the migration must leave it JSON null.
	_, err = rawDB.Exec(`INSERT INTO settings (key, value) VALUES ('applicationTheme', 'nord'), ('oledMode', 'true')`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 68))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'preferences'`).Scan(&columnCount))
	assert.Equal(t, 1, columnCount)

	var theme string
	var oled bool
	var iconCatalog stdsql.NullString
	require.NoError(t, rawDB.QueryRow(`SELECT json_extract(preferences, '$.applicationTheme'), json_extract(preferences, '$.oledMode'), json_extract(preferences, '$.iconCatalog') FROM users WHERE id = 'u-1'`).Scan(&theme, &oled, &iconCatalog))
	assert.Equal(t, "nord", theme)
	assert.True(t, oled)
	assert.False(t, iconCatalog.Valid)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 67))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'preferences'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
}

func TestMigration070_PasskeysAndMFA_UpAndDown(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-passkeys-mfa.db")

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 69))
	var tableCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('passkeys', 'auth_transactions', 'passkey_ceremonies', 'passkey_recovery_codes')`).Scan(&tableCount))
	assert.Zero(t, tableCount)

	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'passkey_mfa_enabled'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 70))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('passkeys', 'auth_transactions', 'passkey_ceremonies', 'passkey_recovery_codes')`).Scan(&tableCount))
	assert.Equal(t, 4, tableCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'passkey_mfa_enabled'`).Scan(&columnCount))
	assert.Equal(t, 1, columnCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('user_sessions') WHERE name IN ('mfa_method', 'mfa_verified_at')`).Scan(&columnCount))
	assert.Equal(t, 2, columnCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 69))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('passkeys', 'auth_transactions', 'passkey_ceremonies', 'passkey_recovery_codes')`).Scan(&tableCount))
	assert.Zero(t, tableCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'passkey_mfa_enabled'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('user_sessions') WHERE name IN ('mfa_method', 'mfa_verified_at')`).Scan(&columnCount))
	assert.Zero(t, columnCount)
}

// TestMigration073_BackupSupport_PreservesExistingBackups proves that
// pre-existing volume backup rows survive the backup-support migration as
// format=archive, and that downgrading is refused while Rustic rows exist.
func TestMigration073_BackupSupport_PreservesExistingBackups(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-backup-support.db")

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 72))
	_, err := rawDB.ExecContext(ctx, `INSERT INTO volume_backups (id, volume_name, size, created_at) VALUES ('legacy-1', 'app-data', 42, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 73))
	var format, status, destination string
	var size int64
	require.NoError(t, rawDB.QueryRow(`SELECT format, status, destination, size FROM volume_backups WHERE id = 'legacy-1'`).Scan(&format, &status, &destination, &size))
	assert.Equal(t, "archive", format)
	assert.Equal(t, "succeeded", status)
	assert.Equal(t, "local", destination)
	assert.EqualValues(t, 42, size)

	// Downgrade is refused while a Rustic-format row exists.
	_, err = rawDB.ExecContext(ctx, `INSERT INTO volume_backups (id, volume_name, size, created_at, format, local_snapshot_id) VALUES ('rustic-1', 'app-data', 7, CURRENT_TIMESTAMP, 'rustic', 'snap-1')`)
	require.NoError(t, err)
	err = migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 72)
	require.Error(t, err)

	// With only archive rows left, the downgrade succeeds and keeps the row.
	_, err = rawDB.ExecContext(ctx, `DELETE FROM volume_backups WHERE id = 'rustic-1'`)
	require.NoError(t, err)
	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 72))
	var count int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM volume_backups WHERE id = 'legacy-1'`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestMigration072_ProjectTags_UpDownAndCascade(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-project-tags.db")
	rawDB.SetMaxOpenConns(1)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 71))
	var tableCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'project_tags'`).Scan(&tableCount))
	assert.Zero(t, tableCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 72))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'project_tags'`).Scan(&tableCount))
	assert.Equal(t, 1, tableCount)
	var indexCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_project_tags_name'`).Scan(&indexCount))
	assert.Equal(t, 1, indexCount)

	_, err := rawDB.Exec(`PRAGMA foreign_keys=ON`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO projects (id, name, path, status, service_count, running_count, created_at) VALUES ('project-1', 'demo', '/tmp/demo', 'stopped', 0, 0, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO project_tags (project_id, name, source) VALUES ('project-1', 'database', 'ui'), ('project-1', 'database', 'compose')`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO project_tags (project_id, name, source) VALUES ('project-1', 'database', 'ui')`)
	require.Error(t, err)
	_, err = rawDB.Exec(`INSERT INTO project_tags (project_id, name, source) VALUES ('project-1', 'invalid', 'other')`)
	require.Error(t, err)
	_, err = rawDB.Exec(`DELETE FROM projects WHERE id = 'project-1'`)
	require.NoError(t, err)
	var tagCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM project_tags WHERE project_id = 'project-1'`).Scan(&tagCount))
	assert.Zero(t, tagCount)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 71))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'project_tags'`).Scan(&tableCount))
	assert.Zero(t, tableCount)
}

func TestMigration071_RenamesVolumeWorkspaceLegacyKeys(t *testing.T) {
	ctx := context.Background()
	rawDB, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-volume-workspace-keys.db")

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 70))
	_, err := rawDB.Exec(`DELETE FROM settings WHERE key = 'volumeHelperIdleTimeout'`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO settings (key, value) VALUES ('volumeBrowserHelperIdleTimeout', '27')`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO roles (id, name, permissions) VALUES ('role-workspace', 'Workspace role', '["volumes:browse","volumes:read","volumes:upload"]')`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO api_keys (id, name, key_hash, key_prefix) VALUES ('key-workspace', 'Workspace key', 'hash', 'arc_')`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO api_key_permissions (id, api_key_id, permission) VALUES ('grant-browse', 'key-workspace', 'volumes:browse'), ('grant-read', 'key-workspace', 'volumes:read')`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 71))
	var timeout string
	require.NoError(t, rawDB.QueryRow(`SELECT value FROM settings WHERE key = 'volumeHelperIdleTimeout'`).Scan(&timeout))
	assert.Equal(t, "27", timeout)
	var count int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'volumeBrowserHelperIdleTimeout'`).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM json_each((SELECT permissions FROM roles WHERE id = 'role-workspace')) WHERE value = 'volumes:read'`).Scan(&count))
	assert.Equal(t, 1, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM json_each((SELECT permissions FROM roles WHERE id = 'role-workspace')) WHERE value = 'volumes:browse'`).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM api_key_permissions WHERE api_key_id = 'key-workspace' AND permission = 'volumes:read'`).Scan(&count))
	assert.Equal(t, 1, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM api_key_permissions WHERE permission = 'volumes:browse'`).Scan(&count))
	assert.Zero(t, count)

	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 70))
	require.NoError(t, rawDB.QueryRow(`SELECT value FROM settings WHERE key = 'volumeBrowserHelperIdleTimeout'`).Scan(&timeout))
	assert.Equal(t, "27", timeout)
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
	require.ErrorContains(t, err, "newer than this Arcane binary supports")
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
	require.ErrorContains(t, err, "ALLOW_DOWNGRADE=true is not sufficient")
	require.ErrorContains(t, err, "restore the database from a backup")
	require.ErrorContains(t, err, strconv.FormatInt(highestVersion+1, 10))
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
	require.ErrorContains(t, err, "is dirty")
	require.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")

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
	require.ErrorContains(t, err, "is dirty")
	require.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")

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
	require.NoError(t, sqliteutil.RegisterFunctions())

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
	require.ErrorContains(t, err, "failed to insert Goose migration version")

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
	require.ErrorContains(t, err, "dirty")
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

// TestSQLiteMigrations_ColumnAddsAreReversible guards against the historical footgun
// where a SQLite migration adds a column but leaves a no-op '-- +goose Down' (on the
// mistaken belief that SQLite can't DROP COLUMN). The bundled modernc SQLite supports
// DROP COLUMN, so every column-adding migration must reverse itself — otherwise a
// down/up round-trip fails with a duplicate-column error. See 059_add_api_key_kind.sql
// for the expected pattern.
func TestSQLiteMigrations_ColumnAddsAreReversible(t *testing.T) {
	migrationsFS, err := embeddedMigrationFSInternal(dbProviderSQLite)
	require.NoError(t, err)

	entries, err := fs.ReadDir(migrationsFS, ".")
	require.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, entry.Name())
		require.NoError(t, err)

		up, down := gooseUpDownSectionsInternal(string(content))
		if !strings.Contains(strings.ToUpper(up), "ADD COLUMN") {
			continue
		}

		assert.True(t, sectionHasSQLInternal(down),
			"migration %s adds a column but its '-- +goose Down' has no SQL; add the reversing "+
				"ALTER TABLE ... DROP COLUMN (modernc SQLite supports it). A no-op Down breaks "+
				"down/up round-trips with a duplicate-column error.", entry.Name())
	}
}

// TestSQLiteMigrations_DownUpRoundTrip migrates fully up, downgrades to just below
// 029_add_ssh_host_key_verification (whose Down was previously a no-op), then migrates
// back up. It proves that Down block executes cleanly (DROP COLUMN) and that re-applying
// the Up does not fail with a duplicate-column error.
//
// The target stops at 28 rather than below 007 because downgrading past version 25 hits
// a separate, pre-existing problem: 025's Down drops a column from api_keys while a
// foreign key still references it, which SQLite rejects. That is unrelated to the no-op
// Down fixes here and is tracked separately.
func TestSQLiteMigrations_DownUpRoundTrip(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "arcane-roundtrip.db")

	require.NoError(t, migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}))

	const belowFixedVersion = int64(28)
	require.NoError(t, migrateDatabaseToVersionInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, belowFixedVersion))
	assert.Equal(t, belowFixedVersion, readGooseSQLiteVersionInternal(t, dsn))

	// Re-up: a no-op Down would have left the column in place, so this would fail
	// with "duplicate column name".
	require.NoError(t, migrateDatabaseInternal(ctx, rawDB, dbProviderSQLite, MigrationOptions{}))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersionInternal(t, dsn))
}

// gooseUpDownSectionsInternal splits a goose migration into the text before the
// '-- +goose Down' marker (the Up section) and the text after it (the Down section).
func gooseUpDownSectionsInternal(content string) (up, down string) {
	const downMarker = "-- +goose Down"
	before, after, ok := strings.Cut(content, downMarker)
	if !ok {
		return content, ""
	}
	return before, after
}

// sectionHasSQLInternal reports whether a migration section contains at least one
// non-comment, non-blank line (i.e. an actual statement rather than only comments).
func sectionHasSQLInternal(section string) bool {
	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return true
	}
	return false
}

func TestIdentityNormalizationMigration(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		first, second           string
		emailFirst, emailSecond string
	}{
		{name: "identity whitespace preserved", first: " Jose\u0301 ", second: "Other", emailFirst: " é@example.com ", emailSecond: "e\u0301@example.com"},
		{name: "canonically equivalent usernames preserved", first: "Jose\u0301", second: "José"},
		{name: "whitespace username preserved", first: "\t ", second: "Other"},
		{name: "existing duplicate emails preserved", first: "one", second: "two", emailFirst: "a@example.com", emailSecond: "a@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := newSQLiteSQLDBInternal(t, t.TempDir(), "identities.db")
			require.NoError(t, migrateDatabaseToVersionInternal(ctx, db, dbProviderSQLite, MigrationOptions{}, 80))
			const displayName = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000Jose\u0301\u3000"
			_, err := db.Exec("INSERT INTO users (id, username, email, display_name, password_hash) VALUES (?, ?, ?, ?, 'unchanged'), (?, ?, ?, ?, 'unchanged')", "first", tc.first, tc.emailFirst, displayName, "second", tc.second, tc.emailSecond, nil)
			require.NoError(t, err)
			require.NoError(t, migrateDatabaseInternal(ctx, db, dbProviderSQLite, MigrationOptions{}))
			require.NoError(t, migrateDatabaseInternal(ctx, db, dbProviderSQLite, MigrationOptions{}))
			for _, expected := range []struct {
				id, username, email string
				displayName         stdsql.NullString
			}{
				{"first", tc.first, tc.emailFirst, stdsql.NullString{String: "José", Valid: true}},
				{"second", tc.second, tc.emailSecond, stdsql.NullString{}},
			} {
				var id, username, email, password string
				var displayName stdsql.NullString
				require.NoError(t, db.QueryRow("SELECT id, username, email, display_name, password_hash FROM users WHERE id = ?", expected.id).Scan(&id, &username, &email, &displayName, &password))
				require.Equal(t, expected.id, id)
				require.Equal(t, expected.username, username)
				require.Equal(t, expected.email, email)
				require.Equal(t, expected.displayName, displayName)
				require.Equal(t, "unchanged", password)
			}
			var version int64
			require.NoError(t, db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version))
			highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
			require.NoError(t, err)
			require.Equal(t, highest, version)
		})
	}
}

func TestIdentityNormalizationGooseUpgrade(t *testing.T) {
	ctx := context.Background()
	db, dsn := newSQLiteSQLDBInternal(t, t.TempDir(), "upgrade.db")
	require.NoError(t, migrateDatabaseToVersionInternal(ctx, db, dbProviderSQLite, MigrationOptions{}, 80))
	_, err := db.Exec("INSERT INTO users (id, username, display_name, password_hash) VALUES (?, ?, ?, ?), (?, ?, ?, ?)", "first", " Jose\u0301 ", " Jose\u0301 ", "unchanged", "second", "José", nil, "unchanged")
	require.NoError(t, err)
	initialized, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	initializedSQL, err := initialized.DB.DB()
	require.NoError(t, err)
	require.NoError(t, initializedSQL.Close())
	require.NoError(t, migrateDatabaseInternal(ctx, db, dbProviderSQLite, MigrationOptions{}))
	var id, username, password string
	var email, displayName stdsql.NullString
	require.NoError(t, db.QueryRow("SELECT id, username, email, display_name, password_hash FROM users WHERE id = 'first'").Scan(&id, &username, &email, &displayName, &password))
	require.Equal(t, "first", id)
	require.Equal(t, " Jose\u0301 ", username)
	require.False(t, email.Valid)
	require.Equal(t, stdsql.NullString{String: "José", Valid: true}, displayName)
	require.Equal(t, "unchanged", password)
	var version int64
	require.NoError(t, db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version))
	highest, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	require.Equal(t, highest, version)
}

func TestIdentityNormalizationPostgresUpgrade(t *testing.T) {
	dsn := os.Getenv("ARCANE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ARCANE_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	admin, err := stdsql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	schema := fmt.Sprintf("normalization_test_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, err)
	})
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := stdsql.Open("pgx", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migrateDatabaseToVersionInternal(ctx, db, dbProviderPostgres, MigrationOptions{}, 80))
	const whitespace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
	_, err = db.ExecContext(ctx, "INSERT INTO users (id, username, email, display_name, password_hash) VALUES ($1, $2, $3, $4, $5), ($6, $7, $8, $9, $10)", "first", " Jose\u0301 ", " a@example.com ", whitespace+"Jose\u0301"+whitespace, "unchanged", "second", "José", nil, nil, "unchanged")
	require.NoError(t, err)
	require.NoError(t, migrateDatabaseInternal(ctx, db, dbProviderPostgres, MigrationOptions{}))
	require.NoError(t, migrateDatabaseInternal(ctx, db, dbProviderPostgres, MigrationOptions{}))
	for _, expected := range []struct {
		id, username       string
		email, displayName stdsql.NullString
	}{
		{"first", " Jose\u0301 ", stdsql.NullString{String: " a@example.com ", Valid: true}, stdsql.NullString{String: "José", Valid: true}},
		{"second", "José", stdsql.NullString{}, stdsql.NullString{}},
	} {
		var id, username, password string
		var email, displayName stdsql.NullString
		require.NoError(t, db.QueryRowContext(ctx, "SELECT id, username, email, display_name, password_hash FROM users WHERE id = $1", expected.id).Scan(&id, &username, &email, &displayName, &password))
		require.Equal(t, expected.id, id)
		require.Equal(t, expected.username, username)
		require.Equal(t, expected.email, email)
		require.Equal(t, expected.displayName, displayName)
		require.Equal(t, "unchanged", password)
	}
	var version int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied").Scan(&version))
	highest, err := getHighestEmbeddedMigrationVersionInternal("postgres")
	require.NoError(t, err)
	require.Equal(t, highest, version)
}
