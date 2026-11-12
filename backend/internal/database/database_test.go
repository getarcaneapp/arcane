package database

import (
	stdsql "database/sql"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/database/migrations"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/sqlite"
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

func TestMigration100_RemovesDashboardReadPermission(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-dashboard-read.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 99))
	_, err := rawDB.ExecContext(ctx, `INSERT INTO roles (id, name, permissions) VALUES
		('r1', 'Mixed', '["containers:list","dashboard:read","images:list"]'),
		('r2', 'Dashboard only', '["dashboard:read"]'),
		('r3', 'Untouched', '["volumes:list"]')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO api_keys (id, name, key_hash, key_prefix) VALUES ('k1', 'Key', 'hash', 'prefix')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO api_key_permissions (id, api_key_id, permission) VALUES
		('p1', 'k1', 'dashboard:read'),
		('p2', 'k1', 'containers:list')`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 100))
	rolePermissions := map[string]string{}
	rows, err := rawDB.QueryContext(ctx, `SELECT id, permissions FROM roles WHERE id IN ('r1', 'r2', 'r3')`)
	require.NoError(t, err)
	for rows.Next() {
		var id, permissions string
		require.NoError(t, rows.Scan(&id, &permissions))
		rolePermissions[id] = permissions
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.Equal(t, map[string]string{
		"r1": `["containers:list","images:list"]`,
		"r2": `[]`,
		"r3": `["volumes:list"]`,
	}, rolePermissions)
	var apiKeyPermission string
	require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT group_concat(permission) FROM api_key_permissions WHERE api_key_id = 'k1'`).Scan(&apiKeyPermission))
	assert.Equal(t, "containers:list", apiKeyPermission)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 99))
	var r1Permissions string
	require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT permissions FROM roles WHERE id = 'r1'`).Scan(&r1Permissions))
	assert.JSONEq(t, `["containers:list","images:list"]`, r1Permissions)
}

func TestMigration101_ConvertsCronSchedulesToFiveFields(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-cron-five-field.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 100))
	_, err := rawDB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES
		('pollingInterval', '0 0 * * * *'),
		('autoHealInterval', '@every 5m'),
		('scheduledPruneInterval', '30 2 * * *'),
		('projectsDirectory', 'a b c d e f')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO system_backup_policies (id, schedule) VALUES ('s1', '0 0 3 * * *'), ('s2', '@daily')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO volume_backup_policies (id, volume_name, schedule) VALUES ('v1', 'data', '0 */15 * * * *')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('systemVolumeBackupConfig',
		'{"policies":[{"id":"p1","enabled":true,"schedule":"0 0 2 * * *"},{"id":"p2","schedule":"@weekly"},{"id":"p3","schedule":"15 4 * * *"}]}')`)
	require.NoError(t, err)

	volumeConfigSchedules := func() []string {
		var value string
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'systemVolumeBackupConfig'`).Scan(&value))
		var config struct {
			Policies []struct {
				Schedule string `json:"schedule"`
			} `json:"policies"`
		}
		require.NoError(t, json.Unmarshal([]byte(value), &config))
		schedules := make([]string, 0, len(config.Policies))
		for _, policy := range config.Policies {
			schedules = append(schedules, policy.Schedule)
		}
		return schedules
	}

	schedules := func() map[string]string {
		values := map[string]string{}
		rows, queryErr := rawDB.QueryContext(ctx, `
			SELECT key, value FROM settings WHERE key IN ('pollingInterval', 'autoHealInterval', 'scheduledPruneInterval', 'projectsDirectory')
			UNION ALL SELECT id, schedule FROM system_backup_policies
			UNION ALL SELECT id, schedule FROM volume_backup_policies`)
		require.NoError(t, queryErr)
		for rows.Next() {
			var id, value string
			require.NoError(t, rows.Scan(&id, &value))
			values[id] = value
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		return values
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 101))
	assert.Equal(t, map[string]string{
		"pollingInterval":        "0 * * * *",
		"autoHealInterval":       "@every 5m",
		"scheduledPruneInterval": "30 2 * * *",
		"projectsDirectory":      "a b c d e f",
		"s1":                     "0 3 * * *",
		"s2":                     "@daily",
		"v1":                     "*/15 * * * *",
	}, schedules())
	assert.Equal(t, []string{"0 2 * * *", "@weekly", "15 4 * * *"}, volumeConfigSchedules())

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 100))
	assert.Equal(t, map[string]string{
		"pollingInterval":        "0 0 * * * *",
		"autoHealInterval":       "@every 5m",
		"scheduledPruneInterval": "0 30 2 * * *",
		"projectsDirectory":      "a b c d e f",
		"s1":                     "0 0 3 * * *",
		"s2":                     "@daily",
		"v1":                     "0 */15 * * * *",
	}, schedules())
	assert.Equal(t, []string{"0 0 2 * * *", "@weekly", "0 15 4 * * *"}, volumeConfigSchedules())
}

func TestMigration102_CleansUpRBACPermissions(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-rbac-cleanup.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 101))
	pre102Permissions := strings.Fields(`
		users:list users:read users:create users:update users:delete roles:list roles:read apikeys:list
		apikeys:read apikeys:create apikeys:update apikeys:delete federated:list federated:read
		federated:create federated:update federated:delete settings:read settings:write environments:list
		environments:read environments:create environments:update environments:delete environments:pair
		environments:sync registries:list registries:read registries:create registries:update
		registries:delete registries:test registries:browse registries:delete-tags templates:list
		templates:read templates:create templates:update templates:delete variables:read variables:create
		variables:update variables:delete variables:sync git-repositories:list git-repositories:read
		git-repositories:create git-repositories:update git-repositories:delete git-repositories:test
		git-repositories:sync s3-destinations:list s3-destinations:read s3-destinations:create
		s3-destinations:update s3-destinations:delete s3-destinations:test s3-destinations:sync
		system-backups:read system-backups:manage system-backups:restore system-backups:recovery-key
		events:read events:delete customize:manage diagnostics:read containers:list containers:read
		containers:logs containers:create containers:start containers:stop containers:restart
		containers:redeploy containers:edit containers:kill containers:pause containers:delete
		containers:exec containers:autoupdate projects:list projects:read projects:logs projects:create
		projects:update projects:deploy projects:down projects:restart projects:delete projects:archive
		images:list images:read images:pull images:push images:build images:tag images:commit images:prune
		images:delete images:upload images:patch volumes:list volumes:read volumes:create volumes:rename
		volumes:delete volumes:prune volumes:upload volumes:backup networks:list networks:read
		networks:create networks:delete networks:prune networks:connect networks:disconnect swarm:read
		swarm:init swarm:join swarm:leave swarm:spec swarm:nodes swarm:services swarm:services:logs
		swarm:stacks swarm:configs swarm:secrets swarm:unlock podman:read podman:pods podman:quadlets
		podman:quadlets:lifecycle podman:kube podman:secrets podman:autoupdate gitops:list gitops:read
		gitops:create gitops:update gitops:delete gitops:sync gitops:lifecycle gitops:backup webhooks:list
		webhooks:create webhooks:update webhooks:delete jobs:manage notifications:manage system:read
		system:prune system:upgrade image-updates:read image-updates:check vulnerabilities:read
		vulnerabilities:scan vulnerabilities:manage build-workspaces:manage activities:read
		activities:cancel activities:delete
	`)
	pre102JSON, err := json.Marshal(pre102Permissions)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO roles (id, name, permissions) VALUES
		('r7', 'Custom admin', ?),
		('r8', 'Builder', '["images:build"]')`, string(pre102JSON))
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO roles (id, name, permissions) VALUES
		('r1', 'Customizer', '["containers:list","customize:manage"]'),
		('r2', 'Checker', '["image-updates:check","images:list"]'),
		('r3', 'Already applies', '["image-updates:check","image-updates:apply"]'),
		('r4', 'Untouched', '["volumes:list"]'),
		('r5', 'Renamed', '["containers:edit","jobs:manage","swarm:read","podman:quadlets:lifecycle","settings:write"]'),
		('r6', 'Settings reader', '["settings:read"]')`)
	require.NoError(t, err)
	catalogRoles := make(map[string]string, len(authz.AllPermissions()))
	for i, permission := range authz.AllPermissions() {
		id := fmt.Sprintf("c%d", i)
		catalogRoles[id] = permission
		_, err = rawDB.ExecContext(ctx, `INSERT INTO roles (id, name, permissions) VALUES (?, ?, ?)`, id, "Catalog "+id, `["`+permission+`"]`)
		require.NoError(t, err)
	}
	_, err = rawDB.ExecContext(ctx, `INSERT INTO environments (id, name, api_url, status, enabled) VALUES ('e1', 'Remote', 'edge://remote', 'online', TRUE)`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO api_keys (id, name, key_hash, key_prefix) VALUES
		('k1', 'Key', 'hash', 'prefix'),
		('k2', 'Admin key', 'hash2', 'prefix2'),
		('k3', 'Builder key', 'hash3', 'prefix3')`)
	require.NoError(t, err)
	for i, permission := range pre102Permissions {
		_, err = rawDB.ExecContext(ctx, `INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id) VALUES (?, 'k2', ?, NULL)`,
			fmt.Sprintf("a%d", i), permission)
		require.NoError(t, err)
	}
	_, err = rawDB.ExecContext(ctx, `INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id) VALUES
		('b1', 'k3', 'images:build', 'e1')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id) VALUES
		('p1', 'k1', 'customize:manage', NULL),
		('p2', 'k1', 'image-updates:check', NULL),
		('p3', 'k1', 'image-updates:check', 'e1'),
		('p4', 'k1', 'containers:list', NULL),
		('p5', 'k1', 'swarm:services', 'e1'),
		('p6', 'k1', 'notifications:manage', NULL),
		('p7', 'k1', 'settings:read', 'e1'),
		('p8', 'k1', 'settings:update', NULL)`)
	require.NoError(t, err)

	rolePermissionsWhere := func(where string) map[string][]string {
		values := map[string][]string{}
		rows, queryErr := rawDB.QueryContext(ctx, `SELECT id, permissions FROM roles WHERE `+where)
		require.NoError(t, queryErr)
		for rows.Next() {
			var id, permissions string
			require.NoError(t, rows.Scan(&id, &permissions))
			var decoded []string
			require.NoError(t, json.Unmarshal([]byte(permissions), &decoded))
			slices.Sort(decoded)
			values[id] = decoded
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		return values
	}
	rolePermissions := func() map[string][]string {
		return rolePermissionsWhere(`id IN ('r1', 'r2', 'r3', 'r4', 'r5', 'r6')`)
	}
	apiKeyPermissionsFor := func(apiKeyID string) []string {
		values := []string{}
		rows, queryErr := rawDB.QueryContext(ctx, `SELECT permission || '@' || COALESCE(environment_id, '') FROM api_key_permissions
			WHERE api_key_id = ? ORDER BY permission, environment_id`, apiKeyID)
		require.NoError(t, queryErr)
		for rows.Next() {
			var value string
			require.NoError(t, rows.Scan(&value))
			values = append(values, value)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		slices.Sort(values)
		return values
	}
	apiKeyPermissions := func() []string {
		return apiKeyPermissionsFor("k1")
	}
	sorted := func(permissions ...string) []string {
		return slices.Sorted(slices.Values(permissions))
	}
	expanded := func(permissions ...string) []string {
		return sorted(authz.ExpandPermissions(permissions)...)
	}
	grants := func(environmentID string, permissions []string) []string {
		out := make([]string, len(permissions))
		for i, permission := range permissions {
			out[i] = permission + "@" + environmentID
		}
		return out
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 102))
	checker := expanded("image-updates:check", "image-updates:apply")
	assert.Equal(t, map[string][]string{
		"r1": {"containers:list"},
		"r2": expanded("image-updates:check", "image-updates:apply", "images:list"),
		"r3": checker,
		"r4": {"volumes:list"},
		"r5": expanded(
			"containers:update",
			"jobs:list", "jobs:read", "jobs:update", "jobs:run", "jobs:restart", "jobs:retry", "jobs:cancel", "jobs:ack",
			"swarm:read",
			"swarm-services:list", "swarm-services:read",
			"swarm-stacks:list", "swarm-stacks:read",
			"swarm-nodes:list", "swarm-nodes:read",
			"swarm-configs:list", "swarm-configs:read",
			"swarm-secrets:list", "swarm-secrets:read",
			"podman-quadlets:start", "podman-quadlets:stop", "podman-quadlets:restart",
			"settings:update", "environment-settings:update",
		),
		"r6": expanded("settings:read", "environment-settings:read"),
	}, rolePermissions())
	renamedCatalogPermissions := []string{
		"settings:read", "settings:update", "variables:read", "system-backups:read", "swarm:read",
		"podman:read", "image-updates:read", "image-updates:check", "vulnerabilities:read", "activities:read",
	}
	for id, permissions := range rolePermissionsWhere(`id LIKE 'c%'`) {
		permission := catalogRoles[id]
		assert.Equal(t, permissions, expanded(permissions...), "%s must be closed under catalog requirements", permission)
		assert.Subset(t, permissions, authz.ExpandPermissions([]string{permission}), permission)
		if !slices.Contains(renamedCatalogPermissions, permission) {
			assert.Equal(t, expanded(permission), permissions, permission)
		}
	}
	assert.Contains(t, rolePermissions()["r5"], "images:read")
	assert.Contains(t, rolePermissions()["r5"], "activities:list")
	assert.Equal(t, sorted(slices.Concat(
		grants("", checker),
		grants("e1", checker),
		grants("", []string{"containers:list"}),
		grants("e1", expanded("swarm-services:create", "swarm-services:update", "swarm-services:delete")),
		grants("", expanded("notifications:list", "notifications:read", "notifications:update", "notifications:delete", "notifications:test")),
		grants("e1", expanded("settings:read", "environment-settings:read")),
		grants("", expanded("settings:update", "environment-settings:update")),
	)...), apiKeyPermissions())
	assert.Equal(t, sorted(authz.AllPermissions()...), rolePermissionsWhere(`id = 'r7'`)["r7"])
	customAdmin := authz.NewPermissionSet()
	customAdmin.AddGlobal(rolePermissionsWhere(`id = 'r7'`)["r7"]...)
	assert.True(t, customAdmin.IsGlobalAdmin())
	assert.Equal(t, sorted(grants("", authz.AllPermissions())...), apiKeyPermissionsFor("k2"))
	assert.Contains(t, rolePermissionsWhere(`id = 'r8'`)["r8"], "build-workspaces:list")
	assert.Contains(t, apiKeyPermissionsFor("k3"), "build-workspaces:list@e1")
	var shortIDs int
	require.NoError(t, rawDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_key_permissions WHERE id NOT IN ('p2', 'p3', 'p4', 'p7', 'p8') AND id NOT LIKE 'a%' AND id <> 'b1' AND length(id) <> 36`,
	).Scan(&shortIDs))
	assert.Zero(t, shortIDs)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 101))
	assert.Equal(t, map[string][]string{
		"r1": {"containers:list"},
		"r2": sorted("image-updates:check", "images:list", "image-updates:read", "activities:read"),
		"r3": sorted("image-updates:check", "image-updates:read", "activities:read"),
		"r4": {"volumes:list"},
		"r5": sorted(
			"containers:edit", "containers:read", "containers:list",
			"images:read", "images:list", "networks:list", "volumes:list",
			"activities:read",
			"jobs:manage",
			"swarm:read",
			"podman:quadlets:lifecycle",
			"settings:write", "settings:read",
		),
		"r6": {"settings:read"},
	}, rolePermissions())
	assert.Equal(t, sorted(
		"activities:read@",
		"activities:read@e1",
		"containers:list@",
		"image-updates:check@",
		"image-updates:check@e1",
		"image-updates:read@",
		"image-updates:read@e1",
		"notifications:manage@",
		"settings:read@",
		"settings:read@e1",
		"settings:write@",
		"swarm:services@e1",
	), apiKeyPermissions())
	preserved := slices.DeleteFunc(slices.Clone(pre102Permissions), func(permission string) bool {
		return permission == "customize:manage"
	})
	assert.Equal(t, sorted(preserved...), rolePermissionsWhere(`id = 'r7'`)["r7"])
	assert.Equal(t, sorted(grants("", preserved)...), apiKeyPermissionsFor("k2"))
	assert.Equal(t, sorted("images:build", "images:list", "images:read", "activities:read"), rolePermissionsWhere(`id = 'r8'`)["r8"])
	assert.Equal(t, grants("e1", sorted("images:build", "images:list", "images:read", "activities:read")), apiKeyPermissionsFor("k3"))
}

func TestMigration104_AddsAgentEnrollmentToken(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-enrollment-tokens.db")
	hasTable := func() bool {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'enrollment_tokens'`).Scan(&count))
		return count == 1
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 104))
	assert.True(t, hasTable())
	_, err := rawDB.ExecContext(ctx, `INSERT INTO enrollment_tokens (id, name, token_hash, token_encrypted) VALUES ('t1', 'first', 'hash', 'enc-1')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO enrollment_tokens (id, name, token_hash, token_encrypted) VALUES ('t2', 'second', 'hash', 'enc-2')`)
	require.Error(t, err, "token hashes are unique")
	_, err = rawDB.ExecContext(ctx, `INSERT INTO enrollment_tokens (id, name, token_hash, token_encrypted, environment_id, max_uses) VALUES ('t3', 'bound', 'hash-3', 'enc-3', 'env-1', 1)`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO enrollment_tokens (id, name, token_hash, token_encrypted, environment_id) VALUES ('t4', 'bound again', 'hash-4', 'enc-4', 'env-1')`)
	require.Error(t, err, "an environment holds at most one bound token")
	_, err = rawDB.ExecContext(ctx, `INSERT INTO enrollment_tokens (id, name, token_hash, token_encrypted) VALUES ('t5', 'unbound', 'hash-5', 'enc-5')`)
	require.NoError(t, err, "unbound tokens are not limited by the environment index")
	_, err = rawDB.ExecContext(ctx, `INSERT INTO enrollment_tokens (id, name, token_hash) VALUES ('t6', 'no secret', 'hash-6')`)
	require.Error(t, err, "the encrypted token is required")

	var useCount int
	var maxUses, environmentID stdsql.NullString
	require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT use_count, max_uses, environment_id FROM enrollment_tokens WHERE id = 't1'`).Scan(&useCount, &maxUses, &environmentID))
	assert.Zero(t, useCount)
	assert.False(t, maxUses.Valid, "max uses defaults to unlimited")
	assert.False(t, environmentID.Valid)

	hasRequestHashColumn := func() bool {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('environments') WHERE name = 'enrollment_request_hash'`).Scan(&count))
		return count == 1
	}
	assert.True(t, hasRequestHashColumn())
	_, err = rawDB.ExecContext(ctx, `INSERT INTO environments (id, name, api_url, status, enabled) VALUES ('e1', 'one', 'agent://e1', 'pending', true), ('e2', 'two', 'agent://e2', 'pending', true)`)
	require.NoError(t, err, "environments without an enrollment request are not limited by the request index")
	_, err = rawDB.ExecContext(ctx, `UPDATE environments SET enrollment_request_hash = 'request' WHERE id = 'e1'`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `UPDATE environments SET enrollment_request_hash = 'request' WHERE id = 'e2'`)
	require.Error(t, err, "an enrollment request belongs to at most one environment")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 103))
	assert.False(t, hasTable())
	assert.False(t, hasRequestHashColumn())
}

func TestMigration105_GlobalVariableInjectIntoContainers_BackfillsExistingAndDowngrades(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-global-variable-inject.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 104))
	_, err := rawDB.ExecContext(ctx, `INSERT INTO global_variables (id, created_at, key, value) VALUES ('var-existing', CURRENT_TIMESTAMP, 'PUID', '1000')`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 105))
	_, err = rawDB.ExecContext(ctx, `INSERT INTO global_variables (id, created_at, key, value) VALUES ('var-new', CURRENT_TIMESTAMP, 'DB_PASSWORD', 'secret')`)
	require.NoError(t, err)

	var existingInjected, newInjected bool
	require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT inject_into_containers FROM global_variables WHERE id = 'var-existing'`).Scan(&existingInjected))
	require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT inject_into_containers FROM global_variables WHERE id = 'var-new'`).Scan(&newInjected))
	assert.True(t, existingInjected, "variables that existed before the migration keep being injected")
	assert.False(t, newInjected)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 104))
	var columnCount int
	require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('global_variables') WHERE name = 'inject_into_containers'`).Scan(&columnCount))
	assert.Zero(t, columnCount)
}

func TestMigration106_AddsImageUpdateCreated(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-image-update-created.db")
	createdColumns := func() int {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('image_updates') WHERE name IN ('current_created', 'latest_created')`).Scan(&count))
		return count
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 106))
	assert.Equal(t, 2, createdColumns())

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 105))
	assert.Zero(t, createdColumns())
}

func TestMigration107_AddsUserBackdrops(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-user-backdrops.db")
	schemaObjects := func() int {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'user_backdrops') +
			(SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'backdrop_hash')`).Scan(&count))
		return count
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 107))
	assert.Equal(t, 2, schemaObjects())

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 106))
	assert.Zero(t, schemaObjects())
}

func TestMigration108_AddsBackupOrphanedAtAndKeepsExistingBackups(t *testing.T) {
	ctx := t.Context()
	orphanedColumns := func(rawDB *stdsql.DB) int {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM pragma_table_info('volume_backups') WHERE name = 'orphaned_at') +
			(SELECT COUNT(*) FROM pragma_table_info('system_backup_runs') WHERE name = 'orphaned_at')`).Scan(&count))
		return count
	}
	retentionSettings := func(rawDB *stdsql.DB) []string {
		rows, err := rawDB.QueryContext(ctx, `SELECT value FROM settings WHERE key = 'unmanagedBackupRetentionDays'`)
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		var values []string
		for rows.Next() {
			var value string
			require.NoError(t, rows.Scan(&value))
			values = append(values, value)
		}
		require.NoError(t, rows.Err())
		return values
	}

	fresh, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-backup-orphaned-fresh.db")
	require.NoError(t, migrateDatabase(ctx, fresh, dbProviderSQLite, MigrationOptions{}, 108))
	assert.Equal(t, 2, orphanedColumns(fresh))
	assert.Empty(t, retentionSettings(fresh), "fresh installs use the setting's default")

	existing, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-backup-orphaned-existing.db")
	require.NoError(t, migrateDatabase(ctx, existing, dbProviderSQLite, MigrationOptions{}, 107))
	_, err := existing.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('instanceId', 'instance-1')`)
	require.NoError(t, err)
	require.NoError(t, migrateDatabase(ctx, existing, dbProviderSQLite, MigrationOptions{}, 108))
	assert.Equal(t, []string{"0"}, retentionSettings(existing), "existing installs keep their backups until an admin opts in")

	require.NoError(t, migrateDatabase(ctx, existing, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 107))
	assert.Zero(t, orphanedColumns(existing))
	assert.Empty(t, retentionSettings(existing))
}

func TestMigration109_AddsGitRepositoryProvider(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-git-repository-provider.db")
	providerColumns := func() int {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('git_repositories') WHERE name = 'provider'`).Scan(&count))
		return count
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 109))
	assert.Equal(t, 1, providerColumns())

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 108))
	assert.Zero(t, providerColumns())
}

func TestMigration110_RenamesEnvironmentsLandingPage(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-landing-page.db")
	landingPage := func(id string) string {
		var page stdsql.NullString
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT json_extract(preferences, '$.defaultLandingPage') FROM users WHERE id = ?`, id).Scan(&page))
		return page.String
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 109))
	_, err := rawDB.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, roles, preferences) VALUES
		('envs', 'envs', 'unused', '[]', '{"defaultLandingPage":"/environments","theme":"dark"}'),
		('other', 'other', 'unused', '[]', '{"defaultLandingPage":"/containers"}'),
		('none', 'none', 'unused', '[]', NULL)`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 110))
	assert.Equal(t, "/agents", landingPage("envs"))
	assert.Equal(t, "/containers", landingPage("other"))
	assert.Empty(t, landingPage("none"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 109))
	assert.Equal(t, "/environments", landingPage("envs"))
	assert.Equal(t, "/containers", landingPage("other"))
}

func TestMigration111_AddsGitOpsComposeOverrideAndEnv(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-gitops-override-env.db")
	syncColumns := func() int {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gitops_syncs') WHERE name IN ('compose_override_path', 'env_content')`).Scan(&count))
		return count
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 111))
	assert.Equal(t, 2, syncColumns())

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 110))
	assert.Zero(t, syncColumns())
}

func TestMigration112_AddsCustomImages(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-custom-images.db")
	schemaObjects := func() int {
		var count int
		require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'custom_images') +
			(SELECT COUNT(*) FROM settings WHERE key IN ('customLogoHash', 'customLogoFullHash', 'customFaviconHash', 'customPwaIconHash'))`).Scan(&count))
		return count
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 112))
	_, err := rawDB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES
		('customLogoHash', 'a'), ('customLogoFullHash', 'b'), ('customFaviconHash', 'c'), ('customPwaIconHash', 'd')`)
	require.NoError(t, err)
	assert.Equal(t, 5, schemaObjects())

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 111))
	assert.Zero(t, schemaObjects())
}

func TestMigration113_SplitsPasswordPolicyIntoGranularSettings(t *testing.T) {
	ctx := t.Context()
	granularKeys := []string{
		"authPasswordMinLength",
		"authPasswordRequireUppercase",
		"authPasswordRequireLowercase",
		"authPasswordRequireNumber",
		"authPasswordRequireSymbol",
	}
	tests := []struct {
		name   string
		preset string
		down   string
		want   []string
	}{
		{name: "basic", preset: "basic", want: []string{"8", "false", "false", "false", "false"}, down: "basic"},
		{name: "standard", preset: "standard", want: []string{"10", "true", "true", "true", "false"}, down: "standard"},
		{name: "strong", preset: "strong", want: []string{"12", "true", "true", "true", "true"}, down: "strong"},
		{name: "unknown", preset: "bogus", want: []string{"12", "true", "true", "true", "true"}, down: "strong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-password-policy.db")
			settingValues := func(keys ...string) []string {
				values := make([]string, 0, len(keys))
				for _, key := range keys {
					var value string
					require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COALESCE(MAX(value), '') FROM settings WHERE key = ?`, key).Scan(&value))
					values = append(values, value)
				}
				return values
			}

			require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 112))
			_, err := rawDB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('authPasswordPolicy', ?)`, tt.preset)
			require.NoError(t, err)

			require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 113))
			assert.Equal(t, tt.want, settingValues(granularKeys...))
			assert.Equal(t, []string{""}, settingValues("authPasswordPolicy"))

			require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 112))
			assert.Equal(t, []string{tt.down}, settingValues("authPasswordPolicy"))
			assert.Equal(t, []string{"", "", "", "", ""}, settingValues(granularKeys...))
		})
	}

	t.Run("no stored policy", func(t *testing.T) {
		rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-password-policy-empty.db")
		countRows := func() int {
			var count int
			require.NoError(t, rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings WHERE key LIKE 'authPassword%'`).Scan(&count))
			return count
		}

		require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, 113))
		assert.Zero(t, countRows())

		require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{AllowDowngrade: true}, 112))
		assert.Zero(t, countRows())
	})
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
	require.NoError(t, sqlite.RegisterFunctions())

	dsn := "file:" + filepath.Join(dirPath, fileName)
	db, err := stdsql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db, dsn
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
		txlock  string
		pragmas []string
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
