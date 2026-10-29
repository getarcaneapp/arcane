package database

import (
	stdsql "database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database/migrations"
	sqliteutil "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/sqlite"
)

func sqliteMigrationVersions(t *testing.T) []int64 {
	t.Helper()
	db, err := stdsql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, nil)
	require.NoError(t, err)
	versions := []int64{}
	for _, source := range provider.ListSources() {
		versions = append(versions, source.Version)
	}
	return versions
}

func TestMigrateDatabase_UpgradesV2Database(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-v2.db")
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, migrations.BaselineVersion))
	// Arcane v2 records every historical SQL migration below the baseline.
	for version := int64(1); version < migrations.BaselineVersion; version++ {
		_, err := rawDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, true)`, version)
		require.NoError(t, err)
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	versions := sqliteMigrationVersions(t)
	assert.Equal(t, versions[len(versions)-1], readGooseSQLiteVersion(t, dsn))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, migrations.BaselineVersion))
	assert.Equal(t, migrations.BaselineVersion, readGooseSQLiteVersion(t, dsn))
}

func TestMigrateDatabase_RejectsPreBaselineDatabase(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-old.db")
	_, err := rawDB.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, true), (?, true)`, migrations.BaselineVersion-1)
	require.NoError(t, err)

	err = migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion)
	require.ErrorContains(t, err, "upgrade to Arcane v2.17.1 first")
	assert.Equal(t, migrations.BaselineVersion-1, readGooseSQLiteVersion(t, dsn))
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
	require.ErrorContains(t, err, "ARCANE_ALLOW_DOWNGRADE=true")
	require.ErrorContains(t, err, "newer than this Arcane binary supports")

	highestVersionVersions := sqliteMigrationVersions(t)
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
}

func TestMigration096_EdgeOnlyEnvironments_ConvertsDirectAndDowngrades(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-edge-only.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 95))
	_, err := rawDB.Exec(`INSERT INTO environments (id, name, api_url, status, enabled, is_edge, access_token) VALUES
		('0', 'Local', 'http://localhost:3552', 'online', TRUE, 0, NULL),
		('d1', 'Prod Server', 'http://prod-1:3553', 'online', TRUE, 0, 'token-d1'),
		('d2', 'prod  server!', 'http://prod-2:3553', 'offline', TRUE, 0, 'token-d2'),
		('d3', '', 'http://blank:3553', 'online', TRUE, 0, 'token-d3'),
		('d4', 'Ünïcode Name', 'http://unicode:3553', 'online', TRUE, 0, 'token-d4'),
		('d5', 'Staging', 'http://staging:3553', 'online', TRUE, 0, 'token-d5'),
		('e1', 'Staging Edge', 'edge://staging', 'online', TRUE, 1, NULL)`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 96))
	type environmentRow struct {
		apiURL      string
		status      string
		accessToken stdsql.NullString
	}
	environments := map[string]environmentRow{}
	rows, err := rawDB.Query(`SELECT id, api_url, status, access_token FROM environments`)
	require.NoError(t, err)
	for rows.Next() {
		var id string
		var row environmentRow
		require.NoError(t, rows.Scan(&id, &row.apiURL, &row.status, &row.accessToken))
		environments[id] = row
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	for id, apiURL := range map[string]string{
		"d1": "edge://prod-server-d1",
		"d2": "edge://prod-server-d2",
		"d3": "edge://d3",
		"d4": "edge://n-code-name",
		"d5": "edge://staging-d5",
	} {
		assert.Equal(t, environmentRow{apiURL: apiURL, status: "pending"}, environments[id], id)
	}
	assert.Equal(t, environmentRow{apiURL: "edge://staging", status: "online"}, environments["e1"])
	assert.Equal(t, environmentRow{apiURL: "http://localhost:3552", status: "online"}, environments["0"])

	var columnCount int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('environments') WHERE name = 'is_edge'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('vulnerability_risk_snapshots') WHERE name IN ('score_average', '"+
			"score_weight', 'exploited_running')").Scan(&columnCount),
	)
	assert.Equal(t, 3, columnCount)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 95))
	isEdge := map[string]int{}
	rows, err = rawDB.Query(`SELECT id, is_edge FROM environments`)
	require.NoError(t, err)
	for rows.Next() {
		var id string
		var edge int
		require.NoError(t, rows.Scan(&id, &edge))
		isEdge[id] = edge
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.Equal(t, map[string]int{"0": 0, "d1": 1, "d2": 1, "d3": 1, "d4": 1, "d5": 1, "e1": 1}, isEdge)
	require.NoError(
		t,
		rawDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('vulnerability_risk_snapshots') WHERE name IN ('score_average', '"+
			"score_weight', 'exploited_running')").Scan(&columnCount),
	)
	assert.Zero(t, columnCount)
}

func TestMigrateDatabase_BlocksFutureGooseVersionWithoutFlag(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-future.db")
	highestVersionVersions := sqliteMigrationVersions(t)
	highestVersion := highestVersionVersions[len(highestVersionVersions)-1]
	_, err := rawDB.ExecContext(ctx, `CREATE TABLE goose_db_version (
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
	highestVersionVersions := sqliteMigrationVersions(t)
	highestVersion := highestVersionVersions[len(highestVersionVersions)-1]
	_, err := rawDB.ExecContext(ctx, `CREATE TABLE goose_db_version (
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
	require.ErrorContains(t, err, "ARCANE_ALLOW_DOWNGRADE=true is not sufficient")
	require.ErrorContains(t, err, "restore the database from a backup")
	require.ErrorContains(t, err, strconv.FormatInt(highestVersion+1, 10))
	assert.Equal(t, highestVersion+1, readGooseSQLiteVersion(t, dsn))
}

func downgradeTargetVersion(t *testing.T) int64 {
	t.Helper()

	allVersions := sqliteMigrationVersions(t)

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

	highestVersions := sqliteMigrationVersions(t)
	highest := highestVersions[len(highestVersions)-1]
	assert.Equal(t, highest, readGooseSQLiteVersion(t, dsn))

	for _, phase := range []string{"fresh", "reopened"} {
		t.Run(phase+" pool enforces foreign keys", func(t *testing.T) {
			sqlDB, poolErr := db.DB.DB()
			require.NoError(t, poolErr)
			connections := make([]*stdsql.Conn, 0, sqliteWriteConns)
			for range sqliteWriteConns {
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
		require.NoError(t, db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var queryOnly int
			require.NoError(t, tx.Raw("PRAGMA query_only").Scan(&queryOnly).Error)
			require.Equal(t, 1, queryOnly, "read-only transactions must use the deferred reader pool")
			return nil
		}, &stdsql.TxOptions{Isolation: stdsql.LevelRepeatableRead, ReadOnly: true}))
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

func TestSQLiteMigrations_DownUpRoundTrip(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-roundtrip.db")
	rawDB.SetMaxOpenConns(1)
	versions := sqliteMigrationVersions(t)
	targets := append([]int64{}, versions...)
	for i := len(versions) - 2; i >= 0; i-- {
		targets = append(targets, versions[i])
	}
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
			require.ElementsMatch(t, []string{"foreign_keys(1)", "synchronous(NORMAL)", "busy_timeout(2500)", "journal_mode(WAL)", "cache_size(-2000)", "temp_store(MEMORY)"}, parsed.Query()["_pragma"])
			require.Empty(t, parsed.Query().Get("_fk"))
			require.Empty(t, parsed.Query().Get("_foreign_keys"))
			require.Empty(t, parsed.Query().Get("_busy_timeout"))
		})
	}
}

func TestSQLiteDSN_ConcurrencyDefaults(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		pragmas []string
		txlock  string
	}{
		{
			name:    "defaults added",
			input:   "file:arcane.db",
			pragmas: []string{"foreign_keys(1)", "busy_timeout(10000)", "journal_mode(WAL)", "synchronous(NORMAL)", "cache_size(-2000)", "temp_store(MEMORY)"},
			txlock:  "immediate",
		},
		{
			name:    "explicit values kept",
			input:   "file:arcane.db?_journal_mode=DELETE&_timeout=500&_txlock=deferred",
			pragmas: []string{"foreign_keys(1)", "busy_timeout(500)", "journal_mode(DELETE)", "synchronous(NORMAL)", "cache_size(-2000)", "temp_store(MEMORY)"},
			txlock:  "deferred",
		},
		{
			name:    "explicit pragmas kept",
			input:   "file:arcane.db?_pragma=journal_mode(TRUNCATE)&_pragma=busy_timeout(2000)",
			pragmas: []string{"foreign_keys(1)", "busy_timeout(2000)", "journal_mode(TRUNCATE)", "synchronous(NORMAL)", "cache_size(-2000)", "temp_store(MEMORY)"},
			txlock:  "immediate",
		},
		{
			name:    "read-only skips WAL and txlock",
			input:   "file:arcane.db?mode=ro",
			pragmas: []string{"foreign_keys(1)", "busy_timeout(10000)"},
		},
		{
			name:    "memory path skips WAL and txlock",
			input:   "file::memory:?cache=shared",
			pragmas: []string{"foreign_keys(1)", "busy_timeout(10000)"},
		},
		{
			name:    "memory mode skips WAL and txlock",
			input:   "file:arcane?mode=memory&cache=shared",
			pragmas: []string{"foreign_keys(1)", "busy_timeout(10000)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := ParseSQLiteConnectionString(tt.input)
			require.NoError(t, err)
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.pragmas, parsed.Query()["_pragma"])
			require.Equal(t, tt.txlock, parsed.Query().Get("_txlock"))
		})
	}
}
