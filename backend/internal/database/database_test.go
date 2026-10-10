package database

import (
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sqliteutil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/sqlite"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
)

func TestGetEmbeddedMigrationVersions_ProvidersMatch(t *testing.T) {
	sqliteVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)

	postgresVersions, err := getEmbeddedMigrationVersions("postgres")
	require.NoError(t, err)

	assert.Equal(t, sqliteVersions, postgresVersions)
	require.NotEmpty(t, sqliteVersions)
}

func TestInitialize_CreatesSQLiteDirectory(t *testing.T) {
	tempDir := t.TempDir()
	dsn := "file:" + filepath.Join(tempDir, "nested", "arcane-test.db")

	db, err := Initialize(t.Context(), dsn, MigrationOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.DirExists(t, filepath.Join(tempDir, "nested"))
	require.NoDirExists(t, filepath.Join("var", "folders"))
}

func TestMigrateDatabase_BlocksDowngradeWithoutFlag(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-test.db")
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	targetVersion := downgradeTargetVersion(t)

	err := migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, targetVersion)
	require.Error(t, err)
	require.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")
	require.ErrorContains(t, err, "newer than this Arcane binary supports")

	highestVersionVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersionVersions)
	highestVersion := highestVersionVersions[len(highestVersionVersions)-1]
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

func TestMigrateDatabase_DowngradesWhenAllowed(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-test.db")
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	targetVersion := downgradeTargetVersion(t)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, targetVersion))
	assert.Equal(t, targetVersion, readGooseSQLiteVersion(t, dsn))

	t.Run("authorization cleanup survives downgrade and re-upgrade", func(t *testing.T) {
		cleanupCtx := t.Context()
		orphanDB, orphanDSN := newSQLiteSQLDB(t, t.TempDir(), "arcane-orphaned-authorization.db")
		orphanDB.SetMaxOpenConns(1)
		require.NoError(t, migrateDatabase(cleanupCtx, orphanDB, dbProviderSQLite, MigrationOptions{}, 91))
		_, err := orphanDB.ExecContext(cleanupCtx, `PRAGMA foreign_keys=OFF`)
		require.NoError(t, err)
		_, err = orphanDB.ExecContext(cleanupCtx, `
			INSERT INTO users (id, username, password_hash, roles) VALUES ('user', 'user', 'unused', '[]');
			INSERT INTO roles (id, name) VALUES ('role', 'Custom');
			INSERT INTO environments (id, name, api_url) VALUES ('env', 'Env', 'http://env');
			INSERT INTO api_keys (id, name, key_hash, key_prefix, user_id, environment_id) VALUES
				('personal', 'Personal', 'hash-1', 'arc_1', 'user', NULL),
				('system', 'System', 'hash-2', 'arc_2', NULL, NULL),
				('environment', 'Environment', 'hash-3', 'arc_3', NULL, 'env'),
				('missing-user', 'Missing user', 'hash-4', 'arc_4', 'missing', 'env'),
				('missing-env', 'Missing env', 'hash-5', 'arc_5', 'user', 'missing'),
				('missing-both', 'Missing both', 'hash-6', 'arc_6', 'missing', 'missing');
			INSERT INTO user_role_assignments (id, user_id, role_id, environment_id, source) VALUES
				('global', 'user', 'role', NULL, 'manual'),
				('oidc', 'user', 'role', NULL, 'oidc'),
				('scoped', 'user', 'role', 'env', 'manual'),
				('missing-user', 'missing', 'role', NULL, 'manual'),
				('missing-role', 'user', 'missing', NULL, 'manual'),
				('missing-env', 'user', 'role', 'missing', 'manual');
			INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id) VALUES
				('global', 'personal', 'containers:read', NULL),
				('scoped', 'personal', 'containers:read', 'env'),
				('system', 'system', 'containers:read', NULL),
				('environment', 'environment', 'containers:read', 'env'),
				('missing-key', 'missing', 'containers:read', NULL),
				('missing-env', 'personal', 'containers:read', 'missing'),
				('missing-owner', 'missing-user', 'containers:read', 'env'),
				('missing-key-env', 'missing-env', 'containers:read', NULL),
				('missing-both', 'missing-both', 'containers:read', 'missing');
			INSERT INTO oidc_role_mappings (id, claim_value, role_id, environment_id) VALUES
				('global', 'global', 'role', NULL),
				('scoped', 'scoped', 'role', 'env'),
				('missing-role', 'missing-role', 'missing', NULL),
				('missing-env', 'missing-env', 'role', 'missing');
		`)
		require.NoError(t, err)
		var violations int
		require.NoError(t, orphanDB.QueryRowContext(cleanupCtx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations))
		require.Positive(t, violations)
		require.NoError(t, orphanDB.Close())

		db, err := Initialize(cleanupCtx, orphanDSN+"?_fk=0", MigrationOptions{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		assertClean := func() {
			t.Helper()
			for table, expectedIDs := range map[string][]string{
				"users":                 {"user"},
				"roles":                 {"role"},
				"environments":          {"env"},
				"api_keys":              {"environment", "personal", "system"},
				"user_role_assignments": {"global", "oidc", "scoped"},
				"api_key_permissions":   {"environment", "global", "scoped", "system"},
				"oidc_role_mappings":    {"global", "scoped"},
			} {
				ids := []string{}
				require.NoError(t, db.Table(table).Order("id").Pluck("id", &ids).Error)
				require.Equal(t, expectedIDs, ids, table)
			}
			require.NoError(t, db.Raw(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations).Error)
			require.Zero(t, violations)
		}
		assertClean()

		require.NoError(t, db.Close())
		db, err = Initialize(cleanupCtx, orphanDSN, MigrationOptions{})
		require.NoError(t, err)
		assertClean()
		sqlDB, err := db.SQLDB()
		require.NoError(t, err)
		require.NoError(t, migrateDatabase(cleanupCtx, sqlDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 91))
		assertClean()
		require.NoError(t, migrateDatabase(cleanupCtx, sqlDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
		assertClean()
	})
}

func TestMigration065_ProjectBuildImageRefs_UpAndDown(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-project-build-refs.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 64))
	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name = 'build_image_refs_json'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 65))
	var notNull int
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*), COALESCE(MAX(\"notnull\"), 0) FROM pragma_table_info('projects') WHERE name = 'build_"+
			"image_refs_json'").Scan(
			&columnCount,
			&notNull,
		),
	)
	assert.Equal(t, 1, columnCount)
	assert.Zero(t, notNull)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 64))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name = 'build_image_refs_json'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
}

func TestMigration066_GlobalVariables_UpAndDown(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-global-variables.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 65))
	var tableCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('global_variables', 'global_variable_environments')`).Scan(&tableCount))
	assert.Zero(t, tableCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 66))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('global_variables', 'global_variable_environments')`).Scan(&tableCount))
	assert.Equal(t, 2, tableCount)

	_, err := rawDB.Exec(`INSERT INTO environments (id, api_url, status, enabled) VALUES ('env-1', 'http://localhost', 'online', TRUE)`)
	require.NoError(t, err)
	_, err = rawDB.Exec("INSERT INTO global_variables (id, created_at, key, value, is_secret, all_environments) VALUES ('var-" +
		"1', CURRENT_TIMESTAMP, 'API_URL', 'https://example.test', FALSE, FALSE)")
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO global_variable_environments (global_variable_id, environment_id) VALUES ('var-1', 'env-1')`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 65))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('global_variables', 'global_variable_environments')`).Scan(&tableCount))
	assert.Zero(t, tableCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 66))
	var rowCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM global_variables`).Scan(&rowCount))
	assert.Zero(t, rowCount)
}

func TestMigration088_VulnerabilityRisk_BackfillsCVSSAndDowngrades(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-vulnerability-risk.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 87))
	_, err := rawDB.Exec(`INSERT INTO vulnerability_scans (id, image_name, status, scan_time) VALUES ('img-1', 'nginx:latest', 'completed', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO vulnerability_scan_items (image_id, vulnerability_id, pkg_name, severity, details) VALUES
		('img-1', 'CVE-V3', 'openssl', 'HIGH', '{"cvss":{"v2Score":5,"v3Score":7.5}}'),
		('img-1', 'CVE-V2', 'curl', 'MEDIUM', '{"cvss":{"v2Score":4.3}}'),
		('img-1', 'CVE-NONE', 'zlib', 'LOW', NULL)`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 88))
	scores := map[string]stdsql.NullFloat64{}
	rows, err := rawDB.Query(`SELECT vulnerability_id, cvss_score FROM vulnerability_scan_items`)
	require.NoError(t, err)
	for rows.Next() {
		var id string
		var score stdsql.NullFloat64
		require.NoError(t, rows.Scan(&id, &score))
		scores[id] = score
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.InDelta(t, 7.5, scores["CVE-V3"].Float64, 0.001)
	assert.InDelta(t, 4.3, scores["CVE-V2"].Float64, 0.001)
	assert.False(t, scores["CVE-NONE"].Valid)

	var tableCount int
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('vulnerability_threat_intel', '"+
			"vulnerability_risk_snapshots')").Scan(&tableCount),
	)
	assert.Equal(t, 2, tableCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 87))
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('vulnerability_threat_intel', '"+
			"vulnerability_risk_snapshots')").Scan(&tableCount),
	)
	assert.Zero(t, tableCount)
}

func TestMigration067_ActivityBatchID_UpAndDown(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-activity-batch-id.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 66))
	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('activities') WHERE name = 'batch_id'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 67))
	var notNull int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*), COALESCE(MAX("notnull"), 0) FROM pragma_table_info('activities') WHERE name = 'batch_id'`).Scan(&columnCount, &notNull))
	assert.Equal(t, 1, columnCount)
	assert.Zero(t, notNull)

	var indexCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_activities_environment_batch'`).Scan(&indexCount))
	assert.Equal(t, 1, indexCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 66))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('activities') WHERE name = 'batch_id'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_activities_environment_batch'`).Scan(&indexCount))
	assert.Zero(t, indexCount)
}

func TestMigration068_UserPreferences_UpAndDown(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-user-preferences.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 67))
	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'preferences'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	_, err := rawDB.Exec(`INSERT INTO users (id, created_at, updated_at, username, password_hash) VALUES ('u-1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'kyle', 'hash')`)
	require.NoError(t, err)
	// One string setting and one boolean setting are seeded; iconCatalog is
	// deliberately absent so the migration must leave it JSON null.
	_, err = rawDB.Exec(`INSERT INTO settings (key, value) VALUES ('applicationTheme', 'nord'), ('oledMode', 'true')`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 68))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'preferences'`).Scan(&columnCount))
	assert.Equal(t, 1, columnCount)

	var theme string
	var oled bool
	var iconCatalog stdsql.NullString
	require.NoError(
		t,
		rawDB.QueryRow("SELECT json_extract(preferences, '$.applicationTheme'), json_extract(preferences, '$.oledMode'), jso"+
			"n_extract(preferences, '$.iconCatalog') FROM users WHERE id = 'u-1'").Scan(
			&theme,
			&oled,
			&iconCatalog,
		),
	)
	assert.Equal(t, "nord", theme)
	assert.True(t, oled)
	assert.False(t, iconCatalog.Valid)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 67))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'preferences'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
}

func TestMigration070_PasskeysAndMFA_UpAndDown(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-passkeys-mfa.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 69))
	var tableCount int
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('passkeys', 'auth_transactions'"+
			", 'passkey_ceremonies', 'passkey_recovery_codes')").Scan(&tableCount),
	)
	assert.Zero(t, tableCount)

	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'passkey_mfa_enabled'`).Scan(&columnCount))
	assert.Zero(t, columnCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 70))
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('passkeys', 'auth_transactions'"+
			", 'passkey_ceremonies', 'passkey_recovery_codes')").Scan(&tableCount),
	)
	assert.Equal(t, 4, tableCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'passkey_mfa_enabled'`).Scan(&columnCount))
	assert.Equal(t, 1, columnCount)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('user_sessions') WHERE name IN ('mfa_method', 'mfa_verified_at')`).Scan(&columnCount))
	assert.Equal(t, 2, columnCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 69))
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('passkeys', 'auth_transactions'"+
			", 'passkey_ceremonies', 'passkey_recovery_codes')").Scan(&tableCount),
	)
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
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-backup-support.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 72))
	_, err := rawDB.ExecContext(ctx, `INSERT INTO volume_backups (id, volume_name, size, created_at) VALUES ('legacy-1', 'app-data', 42, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 73))
	var format, status, destination string
	var size int64
	require.NoError(t, rawDB.QueryRow(`SELECT format, status, destination, size FROM volume_backups WHERE id = 'legacy-1'`).Scan(&format, &status, &destination, &size))
	assert.Equal(t, "archive", format)
	assert.Equal(t, "succeeded", status)
	assert.Equal(t, "local", destination)
	assert.EqualValues(t, 42, size)

	// Downgrade is refused while a Rustic-format row exists.
	_, err = rawDB.ExecContext(
		ctx,
		"INSERT INTO volume_backups (id, volume_name, size, created_at, format, local_snapshot_id) VALUES ('r"+
			"ustic-1', 'app-data', 7, CURRENT_TIMESTAMP, 'rustic', 'snap-1')",
	)
	require.NoError(t, err)
	err = migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 72)
	require.Error(t, err)

	// With only archive rows left, the downgrade succeeds and keeps the row.
	_, err = rawDB.ExecContext(ctx, `DELETE FROM volume_backups WHERE id = 'rustic-1'`)
	require.NoError(t, err)
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 72))
	var count int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM volume_backups WHERE id = 'legacy-1'`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestMigration072_ProjectTags_UpDownAndCascade(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-project-tags.db")
	rawDB.SetMaxOpenConns(1)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 71))
	var tableCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'project_tags'`).Scan(&tableCount))
	assert.Zero(t, tableCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 72))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'project_tags'`).Scan(&tableCount))
	assert.Equal(t, 1, tableCount)
	var indexCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_project_tags_name'`).Scan(&indexCount))
	assert.Equal(t, 1, indexCount)

	_, err := rawDB.Exec(`PRAGMA foreign_keys=ON`)
	require.NoError(t, err)
	_, err = rawDB.Exec("INSERT INTO projects (id, name, path, status, service_count, running_count, created_at) VALUES ('pro" +
		"ject-1', 'demo', '/tmp/demo', 'stopped', 0, 0, CURRENT_TIMESTAMP)")
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

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 71))
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'project_tags'`).Scan(&tableCount))
	assert.Zero(t, tableCount)
}

func TestMigration071_RenamesVolumeWorkspaceLegacyKeys(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-volume-workspace-keys.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 70))
	_, err := rawDB.Exec(`DELETE FROM settings WHERE key = 'volumeHelperIdleTimeout'`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO settings (key, value) VALUES ('volumeBrowserHelperIdleTimeout', '27')`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO roles (id, name, permissions) VALUES ('role-workspace', 'Workspace role', '["volumes:browse","volumes:read","volumes:upload"]')`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO api_keys (id, name, key_hash, key_prefix) VALUES ('key-workspace', 'Workspace key', 'hash', 'arc_')`)
	require.NoError(t, err)
	_, err = rawDB.Exec("INSERT INTO api_key_permissions (id, api_key_id, permission) VALUES ('grant-browse', 'key-workspace'" +
		", 'volumes:browse'), ('grant-read', 'key-workspace', 'volumes:read')")
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 71))
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

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 70))
	require.NoError(t, rawDB.QueryRow(`SELECT value FROM settings WHERE key = 'volumeBrowserHelperIdleTimeout'`).Scan(&timeout))
	assert.Equal(t, "27", timeout)
}

func TestMigrateDatabase_BlocksFutureGooseVersionWithoutFlag(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-future.db")
	highestVersionVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersionVersions)
	highestVersion := highestVersionVersions[len(highestVersionVersions)-1]
	_, err = rawDB.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, true), (?, true)`, highestVersion+1)
	require.NoError(t, err)

	err = migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion)
	require.Error(t, err)
	require.ErrorContains(t, err, "newer than this Arcane binary supports")
	assert.Equal(t, highestVersion+1, readGooseSQLiteVersion(t, dsn))
}

func TestMigrateDatabase_BlocksDowngradeWhenEmbeddedMigrationMissing(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-missing-down.db")
	highestVersionVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersionVersions)
	highestVersion := highestVersionVersions[len(highestVersionVersions)-1]
	_, err = rawDB.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, true), (?, true)`, highestVersion+1)
	require.NoError(t, err)

	err = migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, latestMigrationVersion)
	require.Error(t, err)
	require.ErrorContains(t, err, "ALLOW_DOWNGRADE=true is not sufficient")
	require.ErrorContains(t, err, "restore the database from a backup")
	require.ErrorContains(t, err, strconv.FormatInt(highestVersion+1, 10))
	assert.Equal(t, highestVersion+1, readGooseSQLiteVersion(t, dsn))
}

func TestAdoptLegacyMigrationState_DirtyVersions(t *testing.T) {
	highestVersions, err := getEmbeddedMigrationVersions(dbProviderSQLite)
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	for _, tc := range []struct {
		name    string
		version int64
	}{
		{name: "current version", version: highest},
		{name: "older version", version: downgradeTargetVersion(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db, dsn := newSQLiteSQLDB(t, t.TempDir(), "legacy-dirty.db")
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, tc.version))
			_, clearErr := db.ExecContext(ctx, "DELETE FROM "+gooseVersionTable)
			require.NoError(t, clearErr)
			seedLegacyMigrationState(t, dsn, tc.version, true)

			adoptionErr := adoptLegacyMigrationState(ctx, db, dbProviderSQLite, MigrationOptions{})
			require.ErrorContains(t, adoptionErr, "is dirty")
			require.ErrorContains(t, adoptionErr, "ALLOW_DOWNGRADE=true")
			require.NoError(t, adoptLegacyMigrationState(ctx, db, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}))
			assert.Equal(t, tc.version, readGooseSQLiteVersion(t, dsn))
			assertLegacyMigrationClean(t, dsn)
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
			assert.Equal(t, highest, readGooseSQLiteVersion(t, dsn))
		})
	}
}

func downgradeTargetVersion(t *testing.T) int64 {
	t.Helper()

	allVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(allVersions), 2, "need at least 2 migration versions to test downgrade")

	return allVersions[len(allVersions)-2]
}

func newSQLiteSQLDB(t *testing.T, dirPath, fileName string) (*stdsql.DB, string) {
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
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("project references repair=%t", broken), func(t *testing.T) {
			ctx := t.Context()
			db, dsn := newSQLiteSQLDB(t, t.TempDir(), "project-references.db")
			db.SetMaxOpenConns(1)
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, 43))
			_, err := db.ExecContext(ctx, `
				INSERT INTO environments (id, name, api_url) VALUES ('env', 'Environment', 'http://agent');
				INSERT INTO projects (id, name, dir_name, path, status) VALUES ('project', 'Project', 'project', '/project', 'running');
				INSERT INTO git_repositories (id, name, url, auth_type) VALUES ('repo', 'Repository', 'https://example.com/repo', 'none');
				INSERT INTO gitops_syncs (id, name, environment_id, repository_id, branch, compose_path, project_name, project_id, updated_at)
				VALUES ('saved', 'Saved', 'env', 'repo', 'main', 'compose.yaml', 'project', 'project', CURRENT_TIMESTAMP);`)
			require.NoError(t, err)
			targets := []int64{44, 43, 48, 47, latestMigrationVersion}
			if broken {
				// Reproduce the old rename with modern SQLite defaults, retaining the projects table's data and constraints.
				_, err = db.ExecContext(ctx, `PRAGMA foreign_keys=OFF; PRAGMA legacy_alter_table=OFF;
					ALTER TABLE projects RENAME TO projects_old;
					PRAGMA legacy_alter_table=ON;
					ALTER TABLE projects_old RENAME TO projects;
					PRAGMA legacy_alter_table=OFF; PRAGMA foreign_keys=ON;`)
				require.NoError(t, err)
				require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, 91))
				_, err = db.ExecContext(ctx, `INSERT INTO gitops_syncs
					(id, name, environment_id, repository_id, branch, compose_path, project_name, updated_at)
					VALUES ('blocked', 'Blocked', 'env', 'repo', 'main', 'compose.yaml', 'project', CURRENT_TIMESTAMP)`)
				require.ErrorContains(t, err, "projects_old")
				_, err = db.ExecContext(ctx, `UPDATE gitops_syncs SET pre_deploy_script_path = 'hooks/deploy.sh',
					pre_deploy_last_run_output = 'saved output', mode = 'backup', backup_paths = '["/data"]',
					last_backup_snapshot = 'snapshot' WHERE id = 'saved'`)
				require.NoError(t, err)
				targets = []int64{latestMigrationVersion, 91, latestMigrationVersion}
			}
			reopenDSN, err := ParseSQLiteConnectionString(dsn)
			require.NoError(t, err)
			for _, target := range targets {
				// Each step uses a fresh connection so migration 006 cannot mask rename bugs with a leaked pragma.
				require.NoError(t, db.Close())
				reopened, openErr := stdsql.Open("sqlite", reopenDSN)
				require.NoError(t, openErr)
				t.Cleanup(func() { require.NoError(t, reopened.Close()) })
				db = reopened
				db.SetMaxOpenConns(1)
				migrationName := ""
				marker := "ALTER TABLE projects_new RENAME TO projects"
				switch {
				case broken && target == latestMigrationVersion:
					migrationName = "092_remove_orphaned_authorization_records.sql"
					marker = "ALTER TABLE gitops_syncs_new RENAME TO gitops_syncs"
				case target == 43 || target == 44:
					migrationName = "044_allow_nested_project_paths.sql"
				case target == 47:
					migrationName = "048_add_compose_project_name.sql"
				}
				if migrationName != "" {
					migration, readErr := resources.FS.ReadFile("migrations/sqlite/" + migrationName)
					require.NoError(t, readErr)
					up, down := gooseUpDownSections(string(migration))
					section := up
					if target == 43 || target == 47 {
						section = down
					}
					beforeRename, _, found := strings.Cut(section, marker)
					require.True(t, found)
					_, err = db.ExecContext(ctx, beforeRename)
					require.NoError(t, err)
					require.NoError(t, db.Close())
					restored, restoreErr := stdsql.Open("sqlite", reopenDSN)
					require.NoError(t, restoreErr)
					t.Cleanup(func() { require.NoError(t, restored.Close()) })
					db = restored
					db.SetMaxOpenConns(1)
					var saved int
					require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects
						JOIN gitops_syncs ON gitops_syncs.project_id = projects.id
						WHERE gitops_syncs.id = 'saved'`).Scan(&saved))
					require.Equal(t, 1, saved)
				}
				if target == latestMigrationVersion {
					require.NoError(t, db.Close())
					initialized, initErr := Initialize(ctx, reopenDSN, MigrationOptions{})
					require.NoError(t, initErr)
					t.Cleanup(func() { require.NoError(t, initialized.Close()) })
					db, err = initialized.SQLDB()
					require.NoError(t, err)
				} else {
					require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, target))
				}
				var parent string
				require.NoError(t, db.QueryRowContext(ctx, `SELECT "table" FROM pragma_foreign_key_list('gitops_syncs') WHERE "from" = 'project_id'`).Scan(&parent))
				require.Equal(t, "projects", parent)
				var savedProject string
				require.NoError(t, db.QueryRowContext(ctx, `SELECT project_id FROM gitops_syncs WHERE id = 'saved'`).Scan(&savedProject))
				require.Equal(t, "project", savedProject)
				_, err = db.ExecContext(ctx, `INSERT INTO gitops_syncs
					(id, name, environment_id, repository_id, branch, compose_path, project_name, project_id, updated_at)
					VALUES ('new', 'New', 'env', 'repo', 'main', 'compose.yaml', 'project', 'project', CURRENT_TIMESTAMP);
					UPDATE gitops_syncs SET project_id = NULL WHERE id = 'new';
					DELETE FROM gitops_syncs WHERE id = 'new';`)
				require.NoError(t, err)
				var count int
				require.NoError(t, db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&count))
				require.Equal(t, 1, count)
				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&count))
				require.Zero(t, count)
				if broken {
					require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gitops_syncs WHERE id = 'saved'
						AND pre_deploy_script_path = 'hooks/deploy.sh' AND pre_deploy_last_run_output = 'saved output'
						AND mode = 'backup' AND backup_paths = '["/data"]' AND last_backup_snapshot = 'snapshot'`).Scan(&count))
					require.Equal(t, 1, count)
					_, err = db.ExecContext(ctx, `INSERT INTO gitops_syncs
						(id, name, environment_id, repository_id, branch, compose_path, project_name, project_id, mode, updated_at)
						VALUES ('duplicate', 'Duplicate', 'env', 'repo', 'main', 'compose.yaml', 'project', 'project', 'backup', CURRENT_TIMESTAMP)`)
					require.ErrorContains(t, err, "UNIQUE constraint failed")
				}
			}
			_, err = db.ExecContext(ctx, `INSERT INTO project_tags (project_id, name, source) VALUES ('project', 'tag', 'ui');
				DELETE FROM projects WHERE id = 'project';`)
			require.NoError(t, err)
			var count int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gitops_syncs WHERE id = 'saved' AND project_id IS NULL`).Scan(&count))
			require.Equal(t, 1, count)
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_tags`).Scan(&count))
			require.Zero(t, count)
		})
	}

	ctx := t.Context()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-init.db")

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	require.NotNil(t, db)

	var settingsCount int64
	require.NoError(t, db.WithContext(ctx).Table("settings").Count(&settingsCount).Error)

	require.NoError(t, db.Close())
}

func TestInitialize_RecordsGooseVersionOnFreshSQLite(t *testing.T) {
	ctx := t.Context()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-goose-fresh.db")

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	highestVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	assert.Equal(t, highest, readGooseSQLiteVersion(t, dsn))

	for _, phase := range []string{"fresh", "reopened"} {
		t.Run(phase+" pool enforces foreign keys", func(t *testing.T) {
			sqlDB, poolErr := db.SQLDB()
			require.NoError(t, poolErr)
			connections := make([]*stdsql.Conn, 0, 3)
			for range 3 {
				conn, connErr := sqlDB.Conn(ctx)
				require.NoError(t, connErr)
				connections = append(connections, conn)
				t.Cleanup(func() { require.NoError(t, conn.Close()) })
			}
			for _, conn := range connections {
				var enabled int
				require.NoError(t, conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled))
				require.Equal(t, 1, enabled)
				_, insertErr := conn.ExecContext(ctx, `INSERT INTO user_role_assignments (id, user_id, role_id)
					VALUES ('invalid', 'missing-user', 'missing-role')`)
				require.ErrorContains(t, insertErr, "FOREIGN KEY constraint failed")
			}
		})
		if phase == "fresh" {
			require.NoError(t, db.Close())
			db, err = Initialize(ctx, dsn+"?_pragma=foreign_keys(0)", MigrationOptions{})
			require.NoError(t, err)
		}
	}

	for parent, deletedTables := range map[string][]string{
		"users":        {"users", "user_role_assignments", "api_keys", "api_key_permissions"},
		"api_keys":     {"api_keys", "api_key_permissions"},
		"environments": {"environments", "user_role_assignments", "api_keys", "api_key_permissions", "oidc_role_mappings"},
		"roles":        {"roles", "user_role_assignments", "oidc_role_mappings"},
	} {
		t.Run(parent+" deletion cascades", func(t *testing.T) {
			cascadeDB, cascadeErr := Initialize(ctx, "file:"+filepath.Join(t.TempDir(), "cascade.db"), MigrationOptions{})
			require.NoError(t, cascadeErr)
			t.Cleanup(func() { require.NoError(t, cascadeDB.Close()) })
			require.NoError(t, cascadeDB.Exec(`
				INSERT INTO users (id, username, password_hash, roles) VALUES
					('target', 'target', 'unused', '[]'), ('other', 'other', 'unused', '[]');
				INSERT INTO roles (id, name) VALUES ('target', 'Target'), ('other', 'Other');
				INSERT INTO environments (id, name, api_url) VALUES
					('target', 'Target', 'http://target'), ('other', 'Other', 'http://other');
				INSERT INTO api_keys (id, name, key_hash, key_prefix, user_id, environment_id) VALUES
					('target', 'Target', 'hash-target', 'arc_target', 'target', 'target'),
					('other', 'Other', 'hash-other', 'arc_other', 'other', 'other'),
					('system', 'System', 'hash-system', 'arc_system', NULL, NULL);
				UPDATE environments SET api_key_id = 'target' WHERE id = 'target';
				INSERT INTO user_role_assignments (id, user_id, role_id, environment_id) VALUES
					('target', 'target', 'target', 'target'), ('other', 'other', 'other', 'other');
				INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id) VALUES
					('target', 'target', 'containers:read', 'target'),
					('other', 'other', 'containers:read', 'other'),
					('system', 'system', 'containers:read', NULL);
				INSERT INTO oidc_role_mappings (id, claim_value, role_id, environment_id) VALUES
					('target', 'target', 'target', 'target'), ('other', 'other', 'other', 'other');
			`).Error)
			require.NoError(t, cascadeDB.Table(parent).Where("id = ?", "target").Delete(nil).Error)
			for _, table := range []string{
				"users", "api_keys", "environments", "roles",
				"user_role_assignments", "api_key_permissions", "oidc_role_mappings",
			} {
				var count int64
				require.NoError(t, cascadeDB.Table(table).Where("id = ?", "other").Count(&count).Error)
				require.EqualValues(t, 1, count, table)
			}
			for _, table := range deletedTables {
				var count int64
				require.NoError(t, cascadeDB.Table(table).Where("id = ?", "target").Count(&count).Error)
				require.Zero(t, count, table)
			}
			for _, table := range []string{"api_keys", "api_key_permissions"} {
				var count int64
				require.NoError(t, cascadeDB.Table(table).Where("id = ?", "system").Count(&count).Error)
				require.EqualValues(t, 1, count, table)
			}
			var violations int
			require.NoError(t, cascadeDB.Raw(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations).Error)
			require.Zero(t, violations)
		})
	}
}

func TestInitialize_AdoptsCleanLegacyMigrationState(t *testing.T) {
	ctx := t.Context()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-legacy-clean.db")
	highestVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	seedLegacyMigrationState(t, dsn, highest, false)

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	assert.Equal(t, highest, readGooseSQLiteVersion(t, dsn))
	assertLegacyMigrationClean(t, dsn)
}

func TestInitialize_RollsBackFailedLegacyMigrationAdoption(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-legacy-rollback.db")
	highestVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	seedLegacyMigrationState(t, dsn, highest, false)

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

	err = adoptLegacyMigrationState(ctx, rawDB, dbProviderSQLite, MigrationOptions{})
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to insert Goose migration version")

	var rowCount int
	err = rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 0 AND is_applied = 0`).Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}

func TestInitialize_BlocksDirtyLegacyMigrationState(t *testing.T) {
	ctx := t.Context()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-legacy-dirty.db")
	highestVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	seedLegacyMigrationState(t, dsn, highest, true)

	db, err := Initialize(ctx, dsn, MigrationOptions{})
	require.Error(t, err)
	require.Nil(t, db)
	require.ErrorContains(t, err, "dirty")
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")
}

func TestInitialize_ClearsDirtyLegacyMigrationStateWhenAllowed(t *testing.T) {
	ctx := t.Context()
	dsn := "file:" + filepath.Join(t.TempDir(), "arcane-legacy-dirty-allowed.db")
	highestVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	seedLegacyMigrationState(t, dsn, highest, true)

	db, err := Initialize(ctx, dsn, MigrationOptions{AllowDowngrade: true})
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	assert.Equal(t, highest, readGooseSQLiteVersion(t, dsn))
	assertLegacyMigrationClean(t, dsn)
}

func TestInitialize_CreatesQueryPerformanceIndexes(t *testing.T) {
	ctx := t.Context()
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
		assertSQLiteIndexExists(t, db, indexName)
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
		assertSQLiteIndexMissing(t, db, indexName)
	}
}

func assertSQLiteIndexExists(t *testing.T, db *DB, indexName string) {
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

func assertSQLiteIndexMissing(t *testing.T, db *DB, indexName string) {
	t.Helper()

	var count int64

	err := db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?",
		indexName,
	).Scan(&count).Error
	require.NoError(t, err)
	assert.Zero(t, count, "expected index %s to be removed", indexName)
}

func seedLegacyMigrationState(t *testing.T, dsn string, version int64, dirty bool) {
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

func readGooseSQLiteVersion(t *testing.T, dsn string) int64 {
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

func assertLegacyMigrationClean(t *testing.T, dsn string) {
	t.Helper()

	rawDB, err := stdsql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rawDB.Close())
	}()

	var dirty bool
	err = rawDB.QueryRow(`SELECT dirty FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&dirty)
	require.NoError(t, err)
	assert.False(t, dirty)
}

func TestSQLiteMigrations_ColumnAddsAreReversible(t *testing.T) {
	migrationsFS, err := fs.Sub(resources.FS, "migrations/"+dbProviderSQLite)
	require.NoError(t, err)

	entries, err := fs.ReadDir(migrationsFS, ".")
	require.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, readFileErr := fs.ReadFile(migrationsFS, entry.Name())
		require.NoError(t, readFileErr)

		up, down := gooseUpDownSections(string(content))
		if !strings.Contains(strings.ToUpper(up), "ADD COLUMN") {
			continue
		}

		assert.True(
			t,
			sectionHasSQL(down),
			"migration %s adds a column but its '-- +goose Down' has no SQL; add the reversing ALTER TABLE ... DR"+
				"OP COLUMN (modernc SQLite supports it). A no-op Down breaks down/up round-trips with a duplicate-col"+
				"umn error.",
			entry.Name(),
		)
	}
}

func TestSQLiteMigrations_DownUpRoundTrip(t *testing.T) {
	for _, version := range []int64{28, 29} {
		t.Run(fmt.Sprintf("gitops rebuild from %d preserves legacy references", version), func(t *testing.T) {
			ctx := t.Context()
			db, dsn := newSQLiteSQLDB(t, t.TempDir(), "legacy-gitops.db")
			db.SetMaxOpenConns(1)
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, version))
			_, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF;
				INSERT INTO environments (id, name, api_url) VALUES ('env', 'Environment', 'http://agent');
				INSERT INTO git_repositories (id, name, url, auth_type) VALUES ('repo', 'Repository', 'https://example.com/repo', 'none');
				INSERT INTO gitops_syncs (id, name, repository_id, branch, compose_path, project_name, project_id, environment_id, updated_at) VALUES
					('valid', 'Valid', 'repo', 'main', 'compose.yaml', 'valid', NULL, 'env', NULL),
					('project', 'Missing project', 'repo', 'main', 'compose.yaml', 'project', 'deleted-project', 'env', CURRENT_TIMESTAMP),
					('environment', 'Missing environment', 'repo', 'main', 'compose.yaml', 'environment', NULL, 'deleted-env', CURRENT_TIMESTAMP),
					('repository', 'Missing repository', 'deleted-repo', 'main', 'compose.yaml', 'repository', NULL, 'env', CURRENT_TIMESTAMP);
				PRAGMA foreign_keys=ON;`)
			require.NoError(t, err)

			for _, target := range []int64{30, 29, latestMigrationVersion} {
				if target == latestMigrationVersion {
					require.NoError(t, db.Close())
					initialized, initErr := Initialize(ctx, dsn, MigrationOptions{})
					require.NoError(t, initErr)
					t.Cleanup(func() { require.NoError(t, initialized.Close()) })
					db, err = initialized.SQLDB()
					require.NoError(t, err)
				} else {
					require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, target))
				}
				var count int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gitops_syncs WHERE
					(id = 'valid' AND repository_id = 'repo' AND environment_id = 'env' AND project_id IS NULL AND updated_at IS NULL) OR
					(id = 'project' AND repository_id = 'repo' AND environment_id = 'env' AND project_id = 'deleted-project') OR
					(id = 'environment' AND repository_id = 'repo' AND environment_id = 'deleted-env' AND project_id IS NULL) OR
					(id = 'repository' AND repository_id = 'deleted-repo' AND environment_id = 'env' AND project_id IS NULL)`).Scan(&count))
				require.Equal(t, 4, count, "saved syncs after migration to %d", target)
				require.NoError(t, db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&count))
				require.Equal(t, 1, count)
			}
		})
	}

	t.Run("api key rebuild preserves environment pairings", func(t *testing.T) {
		ctx := t.Context()
		db, dsn := newSQLiteSQLDB(t, t.TempDir(), "paired-environment.db")
		db.SetMaxOpenConns(1)
		require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, 39))
		_, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `
			INSERT INTO users (id, username, password_hash) VALUES ('owner', 'owner', 'unused');
			INSERT INTO environments (id, name, api_url) VALUES
				('paired', 'Paired', 'http://paired'), ('unpaired', 'Unpaired', 'http://unpaired');
			INSERT INTO api_keys (id, name, key_hash, key_prefix, user_id, environment_id) VALUES
				('pairing-key', 'Pairing', 'saved-hash', 'arc_pairing', 'owner', 'paired');
			UPDATE environments SET api_key_id = 'pairing-key' WHERE id = 'paired';
		`)
		require.NoError(t, err)

		reopenDSN, err := ParseSQLiteConnectionString(dsn)
		require.NoError(t, err)

		for _, target := range []int64{40, 39, 46, 45, latestMigrationVersion} {
			if target == latestMigrationVersion {
				require.NoError(t, db.Close())
				initialized, initErr := Initialize(ctx, dsn, MigrationOptions{})
				require.NoError(t, initErr)
				t.Cleanup(func() { require.NoError(t, initialized.Close()) })
				db, err = initialized.SQLDB()
				require.NoError(t, err)
			} else {
				migrationName := "040_add_api_key_managed_by.sql"
				if target == 46 {
					require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, 45))
				}
				if target == 45 || target == 46 {
					migrationName = "046_nullable_api_key_user_id.sql"
				}
				migration, readErr := resources.FS.ReadFile("migrations/sqlite/" + migrationName)
				require.NoError(t, readErr)
				up, down := gooseUpDownSections(string(migration))
				section := up
				if target == 39 || target == 45 {
					section = down
				}
				beforeRename, _, found := strings.Cut(section, "ALTER TABLE api_keys_")
				require.True(t, found)
				_, err = db.ExecContext(ctx, beforeRename)
				require.NoError(t, err)
				// Closing before the rename simulates an interrupted rebuild.
				require.NoError(t, db.Close())
				reopened, openErr := stdsql.Open("sqlite", reopenDSN)
				require.NoError(t, openErr)
				t.Cleanup(func() { require.NoError(t, reopened.Close()) })
				db = reopened
				db.SetMaxOpenConns(1)
				var savedKeys int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys
					JOIN environments ON environments.api_key_id = api_keys.id
					WHERE api_keys.id = 'pairing-key' AND key_hash = 'saved-hash'`).Scan(&savedKeys))
				require.Equal(t, 1, savedKeys, "pairing after interrupted migration to %d", target)
				require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, target))
			}
			var count int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM environments
				JOIN api_keys ON api_keys.id = environments.api_key_id
				WHERE environments.id = 'paired' AND api_keys.id = 'pairing-key'
					AND api_keys.key_hash = 'saved-hash' AND api_keys.user_id = 'owner'
					AND api_keys.environment_id = 'paired'`).Scan(&count))
			require.Equal(t, 1, count, "pairing after migration to %d", target)
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM environments
				WHERE id = 'unpaired' AND api_key_id IS NULL`).Scan(&count))
			require.Equal(t, 1, count)
			require.NoError(t, db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&count))
			require.Equal(t, 1, count)
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&count))
			require.Zero(t, count)
		}
	})

	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-roundtrip.db")
	rawDB.SetMaxOpenConns(1)
	versions, err := getEmbeddedMigrationVersions(dbProviderSQLite)
	require.NoError(t, err)
	require.NotEmpty(t, versions)
	targets := append([]int64{}, versions...)
	for i := len(versions) - 2; i >= 0; i-- {
		targets = append(targets, versions[i])
	}
	targets = append(targets, 0)
	targets = append(targets, versions...)
	for step, target := range targets {
		passed := t.Run(fmt.Sprintf("schema step %d version %d", step, target), func(t *testing.T) {
			_, pragmaErr := rawDB.ExecContext(ctx, `PRAGMA foreign_keys=ON; PRAGMA legacy_alter_table=OFF;`)
			require.NoError(t, pragmaErr)
			require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, target))
			var missing int
			require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema AS s
				JOIN pragma_foreign_key_list(s.name) AS fk WHERE s.type = 'table'
				AND NOT EXISTS (SELECT 1 FROM sqlite_schema AS parent WHERE parent.type = 'table' AND parent.name = fk."table")`).Scan(&missing))
			require.Zero(t, missing, "foreign keys must reference existing tables")
			require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&missing))
			require.Zero(t, missing)
		})
		require.True(t, passed)
	}
	assert.Equal(t, versions[len(versions)-1], readGooseSQLiteVersion(t, dsn))
}

// gooseUpDownSections splits a goose migration into the text before the
// '-- +goose Down' marker (the Up section) and the text after it (the Down section).
func gooseUpDownSections(content string) (up, down string) {
	const downMarker = "-- +goose Down"
	before, after, ok := strings.Cut(content, downMarker)
	if !ok {
		return content, ""
	}
	return before, after
}

// sectionHasSQL reports whether a migration section contains at least one
// non-comment, non-blank line (i.e. an actual statement rather than only comments).
func sectionHasSQL(section string) bool {
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
			ctx := t.Context()
			db, _ := newSQLiteSQLDB(t, t.TempDir(), "identities.db")
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, 80))
			const displayName = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000Jose\u0301\u3000"
			_, err := db.Exec(
				"INSERT INTO users (id, username, email, display_name, password_hash) VALUES (?, ?, ?, ?, 'unchanged'"+
					"), (?, ?, ?, ?, 'unchanged')",
				"first",
				tc.first,
				tc.emailFirst,
				displayName,
				"second",
				tc.second,
				tc.emailSecond,
				nil,
			)
			require.NoError(t, err)
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
			require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
			for _, expected := range []struct {
				id, username, email string
				displayName         stdsql.NullString
			}{
				{"first", tc.first, tc.emailFirst, stdsql.NullString{String: "José", Valid: true}},
				{"second", tc.second, tc.emailSecond, stdsql.NullString{}},
			} {
				var id, username, email, password string
				var displayName stdsql.NullString
				require.NoError(
					t,
					db.QueryRow(
						"SELECT id, username, email, display_name, password_hash FROM users WHERE id = ?",
						expected.id,
					).Scan(
						&id,
						&username,
						&email,
						&displayName,
						&password,
					),
				)
				require.Equal(t, expected.id, id)
				require.Equal(t, expected.username, username)
				require.Equal(t, expected.email, email)
				require.Equal(t, expected.displayName, displayName)
				require.Equal(t, "unchanged", password)
			}
			var version int64
			require.NoError(t, db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version))
			highestVersions, err := getEmbeddedMigrationVersions("sqlite")
			require.NoError(t, err)
			require.NotEmpty(t, highestVersions)
			highest := highestVersions[len(highestVersions)-1]
			require.Equal(t, highest, version)
		})
	}
}

func TestIdentityNormalizationGooseUpgrade(t *testing.T) {
	ctx := t.Context()
	db, dsn := newSQLiteSQLDB(t, t.TempDir(), "upgrade.db")
	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, 80))
	_, err := db.Exec(
		"INSERT INTO users (id, username, display_name, password_hash) VALUES (?, ?, ?, ?), (?, ?, ?, ?)",
		"first",
		" Jose\u0301 ",
		" Jose\u0301 ",
		"unchanged",
		"second",
		"José",
		nil,
		"unchanged",
	)
	require.NoError(t, err)
	initialized, err := Initialize(ctx, dsn, MigrationOptions{})
	require.NoError(t, err)
	initializedSQL, err := initialized.DB.DB()
	require.NoError(t, err)
	require.NoError(t, initializedSQL.Close())
	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
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
	highestVersions, err := getEmbeddedMigrationVersions("sqlite")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	require.Equal(t, highest, version)
}

func TestIdentityNormalizationPostgresUpgrade(t *testing.T) {
	dsn := os.Getenv("ARCANE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ARCANE_TEST_POSTGRES_DSN is not set")
	}
	ctx := t.Context()
	admin, err := stdsql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	schema := fmt.Sprintf("normalization_test_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, execContextErr := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, execContextErr)
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
	require.NoError(t, migrateDatabase(ctx, db, dbProviderPostgres, MigrationOptions{}, 80))
	const whitespace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
	_, err = db.ExecContext(
		ctx,
		"INSERT INTO users (id, username, email, display_name, password_hash) VALUES ($1, $2, $3, $4, $5), ($"+
			"6, $7, $8, $9, $10)",
		"first",
		" Jose\u0301 ",
		" a@example.com ",
		whitespace+"Jose\u0301"+whitespace,
		"unchanged",
		"second",
		"José",
		nil,
		nil,
		"unchanged",
	)
	require.NoError(t, err)
	require.NoError(t, migrateDatabase(ctx, db, dbProviderPostgres, MigrationOptions{}, latestMigrationVersion))
	require.NoError(t, migrateDatabase(ctx, db, dbProviderPostgres, MigrationOptions{}, latestMigrationVersion))
	for _, expected := range []struct {
		id, username       string
		email, displayName stdsql.NullString
	}{
		{"first", " Jose\u0301 ", stdsql.NullString{String: " a@example.com ", Valid: true}, stdsql.NullString{String: "José", Valid: true}},
		{"second", "José", stdsql.NullString{}, stdsql.NullString{}},
	} {
		var id, username, password string
		var email, displayName stdsql.NullString
		require.NoError(
			t,
			db.QueryRowContext(
				ctx,
				"SELECT id, username, email, display_name, password_hash FROM users WHERE id = $1",
				expected.id,
			).Scan(
				&id,
				&username,
				&email,
				&displayName,
				&password,
			),
		)
		require.Equal(t, expected.id, id)
		require.Equal(t, expected.username, username)
		require.Equal(t, expected.email, email)
		require.Equal(t, expected.displayName, displayName)
		require.Equal(t, "unchanged", password)
	}
	var version int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied").Scan(&version))
	highestVersions, err := getEmbeddedMigrationVersions("postgres")
	require.NoError(t, err)
	require.NotEmpty(t, highestVersions)
	highest := highestVersions[len(highestVersions)-1]
	require.Equal(t, highest, version)
}

func TestSQLiteDSN(t *testing.T) {
	for name, foreignKeys := range map[string]string{
		"missing":          "",
		"short alias":      "&_fk=0",
		"long alias":       "&_foreign_keys=off",
		"function syntax":  "&_pragma=foreign_keys(0)",
		"equals syntax":    "&_pragma=foreign_keys%3DOFF",
		"case and spaces":  "&_pragma=%20FoReIgN_KeYs%20%3D%200",
		"qualified pragma": "&_pragma=main.foreign_keys(0)",
		"quoted pragma":    "&_pragma=%22foreign_keys%22%3D0",
		"duplicates":       "&_pragma=foreign_keys(1)&_pragma=foreign_keys(1)",
		"conflicting":      "&_fk=0&_foreign_keys=1&_pragma=foreign_keys%3D1&_pragma=foreign_keys(0)",
	} {
		t.Run(name, func(t *testing.T) {
			original := "file:arcane.db?_pragma=synchronous(NORMAL)&_busy_timeout=2500&_txlock=immediate&cache=shared" + foreignKeys
			dsn, err := ParseSQLiteConnectionString(original)
			require.NoError(t, err)
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			require.Equal(t, "immediate", parsed.Query().Get("_txlock"))
			require.Equal(t, "shared", parsed.Query().Get("cache"))
			require.ElementsMatch(t, []string{"foreign_keys(1)", "synchronous(NORMAL)", "busy_timeout(2500)"}, parsed.Query()["_pragma"])
			require.Empty(t, parsed.Query().Get("_fk"))
			require.Empty(t, parsed.Query().Get("_foreign_keys"))
			require.Empty(t, parsed.Query().Get("_busy_timeout"))
		})
	}
}
