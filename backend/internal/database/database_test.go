package database

import (
	"context"
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

func TestMigrateDatabase_BlocksDowngradeWithoutFlag(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabase(driver, "sqlite", MigrationOptions{}))
	targetVersion := downgradeTargetVersionInternal(t)

	err := migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", MigrationOptions{}, targetVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")
	assert.ErrorContains(t, err, "newer than this Arcane binary supports")
}

func TestMigrateDatabase_DowngradesWhenAllowed(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabase(driver, "sqlite", MigrationOptions{}))
	targetVersion := downgradeTargetVersionInternal(t)
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	sourceDriver, err := newEmbeddedMigrationSourceInternal("sqlite")
	require.NoError(t, err)

	require.NoError(t, migrateDatabaseFromSourceInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", highestVersion, targetVersion, "iofs", "test embedded migrate source", sourceDriver))

	instance, checkSourceDriver, err := newEmbeddedMigrateInstanceInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite")
	require.NoError(t, err)
	defer closeMigrateSourceInternal(checkSourceDriver, "test embedded migrate source")
	currentVersion, currentDirty, versionErr := instance.Version()
	require.NoError(t, versionErr)
	assert.Equal(t, targetVersion, currentVersion)
	assert.False(t, currentDirty)
}

func TestMigrateDatabase_DowngradesDirtyStateWhenAllowed(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabase(driver, "sqlite", MigrationOptions{}))
	targetVersion := downgradeTargetVersionInternal(t)
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	highestVersionInt, err := safeUintToIntInternal(highestVersion)
	require.NoError(t, err)
	require.NoError(t, newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db").SetVersion(highestVersionInt, true))
	sourceDriver, err := newEmbeddedMigrationSourceInternal("sqlite")
	require.NoError(t, err)

	require.NoError(t, migrateDatabaseFromSourceInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", highestVersion, targetVersion, "iofs", "test embedded migrate source", sourceDriver))

	instance, checkSourceDriver, err := newEmbeddedMigrateInstanceInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite")
	require.NoError(t, err)
	defer closeMigrateSourceInternal(checkSourceDriver, "test embedded migrate source")
	currentVersion, dirty, versionErr := instance.Version()
	require.NoError(t, versionErr)
	assert.Equal(t, targetVersion, currentVersion)
	assert.False(t, dirty)
}

func TestMigrateDatabase_DirtyCurrentVersionRequiresResolution(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabase(driver, "sqlite", MigrationOptions{}))
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)
	highestVersionInt, err := safeUintToIntInternal(highestVersion)
	require.NoError(t, err)
	require.NoError(t, newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db").SetVersion(highestVersionInt, true))

	err = migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", MigrationOptions{}, highestVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "is dirty")
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")

	require.NoError(t, migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", MigrationOptions{AllowDowngrade: true}, highestVersion))

	instance, sourceDriver, err := newEmbeddedMigrateInstanceInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite")
	require.NoError(t, err)
	defer closeMigrateSourceInternal(sourceDriver, "test embedded migrate source")
	currentVersion, dirty, versionErr := instance.Version()
	require.NoError(t, versionErr)
	assert.Equal(t, highestVersion, currentVersion)
	assert.False(t, dirty)
}

func TestMigrateDatabase_DirtyOlderVersionRequiresResolution(t *testing.T) {
	dbDir := t.TempDir()
	driver := newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db")
	require.NoError(t, migrateDatabase(driver, "sqlite", MigrationOptions{}))
	targetVersion := downgradeTargetVersionInternal(t)
	targetVersionInt, err := safeUintToIntInternal(targetVersion)
	require.NoError(t, err)
	require.NoError(t, newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db").SetVersion(targetVersionInt, true))
	highestVersion, err := getHighestEmbeddedMigrationVersionInternal("sqlite")
	require.NoError(t, err)

	err = migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", MigrationOptions{}, highestVersion)
	require.Error(t, err)
	assert.ErrorContains(t, err, "interrupted forward migration")
	assert.ErrorContains(t, err, "ALLOW_DOWNGRADE=true")

	require.NoError(t, migrateDatabaseToVersionInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite", MigrationOptions{AllowDowngrade: true}, highestVersion))

	instance, sourceDriver, err := newEmbeddedMigrateInstanceInternal(newSQLiteMigrationDriverInternal(t, dbDir, "arcane-test.db"), "sqlite")
	require.NoError(t, err)
	defer closeMigrateSourceInternal(sourceDriver, "test embedded migrate source")
	currentVersion, dirty, versionErr := instance.Version()
	require.NoError(t, versionErr)
	assert.Equal(t, highestVersion, currentVersion)
	assert.False(t, dirty)
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

func TestMigrationOptions_GitHubRefUsesBuildRevision(t *testing.T) {
	assert.Equal(t, "abc123def456", githubRefForRevisionInternal(MigrationOptions{}, "abc123def456"))
	assert.Equal(t, "custom-ref", githubRefForRevisionInternal(MigrationOptions{githubRef: "custom-ref"}, "abc123def456"))
	assert.Equal(t, migrationRepositoryRefFallback, githubRefForRevisionInternal(MigrationOptions{}, "unknown"))
}
