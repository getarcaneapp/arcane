package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	postgresMigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/getarcaneapp/arcane/backend/resources"
)

type DB struct {
	*gorm.DB
}

type MigrationOptions struct {
	AllowDowngrade bool
}

type MigrationStatus struct {
	Provider       string
	CurrentVersion uint
	LatestVersion  uint
	HasVersion     bool
	Dirty          bool
}

var customGormLogger logger.Interface

func SetGormLogger(l logger.Interface) {
	customGormLogger = l
}

func Initialize(ctx context.Context, databaseURL string, options MigrationOptions) (*DB, error) {
	db, err := connectDatabase(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sqlDB, err := db.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	driver, dbProvider, err := newMigrationDriverInternal(db, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration driver: %w", err)
	}

	// Run migrations
	if err := migrateDatabase(driver, dbProvider, options); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Set connection pool settings
	if db.Name() == "postgres" {
		sqlDB.SetMaxIdleConns(15)
		sqlDB.SetMaxOpenConns(50)
	} else {
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetMaxOpenConns(20)
	}
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(3 * time.Minute)

	return db, nil
}

func MigrateToVersion(ctx context.Context, databaseURL string, targetVersion uint) error {
	if targetVersion == 0 {
		return fmt.Errorf("target migration version must be greater than zero")
	}

	db, driver, dbProvider, err := openMigrationDatabaseInternal(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("Failed to close database after migration", "error", closeErr)
		}
	}()

	return migrateDatabaseToVersionInternal(driver, dbProvider, targetVersion)
}

func GetMigrationStatus(ctx context.Context, databaseURL string) (*MigrationStatus, error) {
	db, driver, dbProvider, err := openMigrationDatabaseInternal(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("Failed to close database after reading migration status", "error", closeErr)
		}
	}()

	return getMigrationStatusInternal(driver, dbProvider)
}

func connectDatabase(ctx context.Context, databaseURL string) (*DB, error) {
	var dialector gorm.Dialector

	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		connString, err := parseSqliteConnectionString(databaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse SQLite connection string: %w", err)
		}
		if err := ensureSQLiteDirectory(connString); err != nil {
			return nil, fmt.Errorf("failed to prepare SQLite directory: %w", err)
		}
		dialector = glsqlite.Open(connString)
	case strings.HasPrefix(databaseURL, "postgres"):
		dialector = postgres.Open(databaseURL)
	default:
		return nil, fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	// Retry connection up to 3 times
	var db *gorm.DB
	var err error
	for i := 1; i <= 3; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		db, err = gorm.Open(dialector, &gorm.Config{
			Logger: customGormLogger,
			NowFunc: func() time.Time {
				return time.Now().UTC()
			},
			PrepareStmt:                      true,
			IgnoreRelationshipsWhenMigrating: true,
		})
		if err == nil {
			return &DB{db}, nil
		}

		slog.Info("Failed to initialize database", "attempt", i)
		if i < 3 {
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	return nil, err
}

func openMigrationDatabaseInternal(ctx context.Context, databaseURL string) (*DB, database.Driver, string, error) {
	db, err := connectDatabase(ctx, databaseURL)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := ctx.Err(); err != nil {
		_ = db.Close()
		return nil, nil, "", err
	}

	driver, dbProvider, err := newMigrationDriverInternal(db, databaseURL)
	if err != nil {
		_ = db.Close()
		return nil, nil, "", fmt.Errorf("failed to create migration driver: %w", err)
	}

	return db, driver, dbProvider, nil
}

func newMigrationDriverInternal(db *DB, databaseURL string) (database.Driver, string, error) {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get sql.DB: %w", err)
	}

	dbProvider, err := detectDatabaseProviderInternal(databaseURL)
	if err != nil {
		return nil, "", err
	}

	switch dbProvider {
	case "sqlite":
		driver, err := sqliteMigrate.WithInstance(sqlDB, &sqliteMigrate.Config{})
		if err != nil {
			return nil, "", err
		}
		return driver, dbProvider, nil
	case "postgres":
		driver, err := postgresMigrate.WithInstance(sqlDB, &postgresMigrate.Config{})
		if err != nil {
			return nil, "", err
		}
		return driver, dbProvider, nil
	default:
		return nil, "", fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func detectDatabaseProviderInternal(databaseURL string) (string, error) {
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		return "sqlite", nil
	case strings.HasPrefix(databaseURL, "postgres"):
		return "postgres", nil
	default:
		return "", fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}
}

func migrateDatabase(driver database.Driver, dbProvider string, options MigrationOptions) error {
	requiredVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProvider)
	if err != nil {
		return fmt.Errorf("failed to determine target migration version for %s: %w", dbProvider, err)
	}

	return migrateDatabaseUpToVersionInternal(driver, dbProvider, options, requiredVersion)
}

func migrateDatabaseUpToVersionInternal(driver database.Driver, dbProvider string, options MigrationOptions, requiredVersion uint) error {
	embeddedMigrate, embeddedSource, err := newEmbeddedMigrateInstanceInternal(driver, dbProvider)
	if err != nil {
		return fmt.Errorf("failed to create embedded migration instance: %w", err)
	}
	defer closeMigrateSourceInternal(embeddedSource, "embedded migrate source")

	currentVersion, dirty, hasVersion, err := currentMigrationStateInternal(embeddedMigrate)
	if err != nil {
		return err
	}

	logMigrationStateInternal(dbProvider, currentVersion, requiredVersion, dirty, hasVersion)

	if hasVersion && dirty && currentVersion < requiredVersion {
		if !options.AllowDowngrade {
			return fmt.Errorf("database schema version %d is dirty (interrupted forward migration); resolve it manually or set ALLOW_DOWNGRADE=true to clear the dirty flag and re-apply the migration", currentVersion)
		}

		forceVersion, forceErr := safeUintToIntInternal(currentVersion)
		if forceErr != nil {
			return fmt.Errorf("failed to convert current migration version %d while clearing dirty state: %w", currentVersion, forceErr)
		}

		if err := embeddedMigrate.Force(forceVersion); err != nil {
			return fmt.Errorf("failed to clear dirty migration state for version %d: %w", currentVersion, err)
		}

		slog.Warn("Cleared dirty migration state before re-applying forward migration", "provider", dbProvider, "version", currentVersion)
	}

	if hasVersion && dirty && currentVersion == requiredVersion {
		if !options.AllowDowngrade {
			return fmt.Errorf("database schema version %d is dirty; resolve it manually or set ALLOW_DOWNGRADE=true to clear the dirty flag after verifying the database state", currentVersion)
		}

		forceVersion, forceErr := safeUintToIntInternal(currentVersion)
		if forceErr != nil {
			return fmt.Errorf("failed to convert current migration version %d while clearing dirty state: %w", currentVersion, forceErr)
		}

		if err := embeddedMigrate.Force(forceVersion); err != nil {
			return fmt.Errorf("failed to clear dirty migration state for version %d: %w", currentVersion, err)
		}

		slog.Warn("Cleared dirty migration state at current version because ALLOW_DOWNGRADE=true", "provider", dbProvider, "version", currentVersion)
		logUpToDateStateInternal(embeddedMigrate, dbProvider)
		return nil
	}

	if hasVersion && currentVersion > requiredVersion {
		return fmt.Errorf("database schema version %d is newer than this Arcane binary supports (target %d for %s); run arcane-migrator from a newer Arcane image to downgrade before starting this version", currentVersion, requiredVersion, dbProvider)
	}

	upErr := embeddedMigrate.Migrate(requiredVersion)
	if upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
		return fmt.Errorf("failed to apply embedded migrations for %s: %w", dbProvider, upErr)
	}

	if errors.Is(upErr, migrate.ErrNoChange) {
		logUpToDateStateInternal(embeddedMigrate, dbProvider)
		return nil
	}

	slog.Info("Database migrations completed successfully", "provider", dbProvider, "targetVersion", requiredVersion)
	return nil
}

func migrateDatabaseToVersionInternal(driver database.Driver, dbProvider string, requiredVersion uint) error {
	embeddedMigrate, embeddedSource, err := newEmbeddedMigrateInstanceInternal(driver, dbProvider)
	if err != nil {
		return fmt.Errorf("failed to create embedded migration instance: %w", err)
	}
	defer closeMigrateSourceInternal(embeddedSource, "embedded migrate source")

	currentVersion, dirty, hasVersion, err := currentMigrationStateInternal(embeddedMigrate)
	if err != nil {
		return err
	}

	logMigrationStateInternal(dbProvider, currentVersion, requiredVersion, dirty, hasVersion)

	if dirty {
		return fmt.Errorf("database schema version %d is dirty; resolve the dirty migration state before running arcane-migrator", currentVersion)
	}

	err = embeddedMigrate.Migrate(requiredVersion)
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to migrate database from version %d to %d for %s: %w", currentVersion, requiredVersion, dbProvider, err)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		logUpToDateStateInternal(embeddedMigrate, dbProvider)
		return nil
	}

	slog.Info("Database migration completed successfully", "provider", dbProvider, "fromVersion", currentVersion, "toVersion", requiredVersion)
	return nil
}

func getMigrationStatusInternal(driver database.Driver, dbProvider string) (*MigrationStatus, error) {
	latestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to determine latest embedded migration version for %s: %w", dbProvider, err)
	}

	embeddedMigrate, embeddedSource, err := newEmbeddedMigrateInstanceInternal(driver, dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedded migration instance: %w", err)
	}
	defer closeMigrateSourceInternal(embeddedSource, "embedded migrate source")

	currentVersion, dirty, hasVersion, err := currentMigrationStateInternal(embeddedMigrate)
	if err != nil {
		return nil, err
	}

	return &MigrationStatus{
		Provider:       dbProvider,
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		HasVersion:     hasVersion,
		Dirty:          dirty,
	}, nil
}

func newEmbeddedMigrationSourceInternal(dbProvider string) (source.Driver, error) {
	sourceDriver, err := iofs.New(resources.FS, "migrations/"+dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedded migration source: %w", err)
	}

	return sourceDriver, nil
}

func newEmbeddedMigrateInstanceInternal(driver database.Driver, dbProvider string) (*migrate.Migrate, source.Driver, error) {
	sourceDriver, err := newEmbeddedMigrationSourceInternal(dbProvider)
	if err != nil {
		return nil, nil, err
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "arcane", driver)
	if err != nil {
		if closeErr := sourceDriver.Close(); closeErr != nil {
			slog.Warn("Failed to close embedded migration source after instance creation failure", "provider", dbProvider, "error", closeErr)
		}
		return nil, nil, fmt.Errorf("failed to create migration instance: %w", err)
	}

	return m, sourceDriver, nil
}

func safeUintToIntInternal(value uint) (int, error) {
	if value > uint(math.MaxInt) {
		return 0, fmt.Errorf("value %d exceeds max int", value)
	}

	return int(value), nil
}

func currentMigrationStateInternal(m *migrate.Migrate) (version uint, dirty bool, hasVersion bool, err error) {
	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, fmt.Errorf("failed to determine current migration version: %w", err)
	}

	return version, dirty, true, nil
}

func logUpToDateStateInternal(m *migrate.Migrate, dbProvider string) {
	version, versionDirty, versionErr := m.Version()
	switch {
	case errors.Is(versionErr, migrate.ErrNilVersion):
		slog.Info("Database schema is up to date", "provider", dbProvider, "migrationVersion", 0, "dirty", false)
	case versionErr != nil:
		slog.Info("Database schema is up to date", "provider", dbProvider)
	default:
		slog.Info("Database schema is up to date", "provider", dbProvider, "migrationVersion", version, "dirty", versionDirty)
	}
}

func getHighestEmbeddedMigrationVersionInternal(dbProvider string) (uint, error) {
	versions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("no embedded migrations found for %s", dbProvider)
	}

	return versions[len(versions)-1], nil
}

func getEmbeddedMigrationVersionsInternal(dbProvider string) ([]uint, error) {
	entries, err := resources.FS.ReadDir("migrations/" + dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded migrations for %s: %w", dbProvider, err)
	}

	versionsMap := make(map[uint]struct{})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		migration, parseErr := source.DefaultParse(entry.Name())
		if parseErr != nil {
			continue
		}

		versionsMap[migration.Version] = struct{}{}
	}

	versions := make([]uint, 0, len(versionsMap))
	for version := range versionsMap {
		versions = append(versions, version)
	}
	slices.Sort(versions)

	return versions, nil
}

func logMigrationStateInternal(dbProvider string, currentVersion, requiredVersion uint, dirty, hasVersion bool) {
	if !hasVersion {
		slog.Info("Resolved database migration state", "provider", dbProvider, "currentVersion", 0, "requiredVersion", requiredVersion, "dirty", false)
		return
	}

	slog.Info("Resolved database migration state", "provider", dbProvider, "currentVersion", currentVersion, "requiredVersion", requiredVersion, "dirty", dirty)
}

func closeMigrateSourceInternal(sourceDriver source.Driver, sourceName string) {
	if sourceDriver == nil {
		return
	}

	sourceErr := sourceDriver.Close()
	if sourceErr != nil {
		slog.Warn("Failed to close migration source", "source", sourceName, "error", sourceErr)
	}
}

func parseSqliteConnectionString(connString string) (string, error) {
	if !strings.HasPrefix(connString, "file:") {
		connString = "file:" + connString
	}

	connStringUrl, err := url.Parse(connString)
	if err != nil {
		return "", fmt.Errorf("failed to parse SQLite connection string: %w", err)
	}

	qs := make(url.Values, len(connStringUrl.Query()))
	for k, v := range connStringUrl.Query() {
		switch k {
		case "_auto_vacuum", "_vacuum":
			qs.Add("_pragma", "auto_vacuum("+v[0]+")")
		case "_busy_timeout", "_timeout":
			qs.Add("_pragma", "busy_timeout("+v[0]+")")
		case "_case_sensitive_like", "_cslike":
			qs.Add("_pragma", "case_sensitive_like("+v[0]+")")
		case "_foreign_keys", "_fk":
			qs.Add("_pragma", "foreign_keys("+v[0]+")")
		case "_locking_mode", "_locking":
			qs.Add("_pragma", "locking_mode("+v[0]+")")
		case "_secure_delete":
			qs.Add("_pragma", "secure_delete("+v[0]+")")
		case "_synchronous", "_sync":
			qs.Add("_pragma", "synchronous("+v[0]+")")
		case "_journal_mode":
			qs.Add("_pragma", "journal_mode("+v[0]+")")
		case "_txlock":
			qs.Add("_txlock", v[0])
		default:
			qs[k] = v
		}
	}

	connStringUrl.RawQuery = qs.Encode()
	return connStringUrl.String(), nil
}

// FindEnvironmentIDByApiKey finds the environment ID that is associated with the given API key.
// It queries the api_keys table to validate the key and find the associated environment.
func (db *DB) FindEnvironmentIDByApiKey(ctx context.Context, apiKey string) (string, error) {
	var envID string
	err := db.WithContext(ctx).Table("environments").
		Select("environments.id").
		Joins("INNER JOIN api_keys ON api_keys.id = environments.api_key_id").
		Where("api_keys.key = ?", apiKey).
		Pluck("environments.id", &envID).Error
	if err != nil {
		return "", err
	}
	if envID == "" {
		return "", gorm.ErrRecordNotFound
	}
	return envID, nil
}

func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Create parent directory for file-based SQLite if needed
func ensureSQLiteDirectory(connString string) error {
	if !strings.HasPrefix(connString, "file:") {
		return nil
	}
	u, err := url.Parse(connString)
	if err != nil {
		return fmt.Errorf("failed to parse SQLite DSN: %w", err)
	}

	// For "file:data/arcane.db?...", path is in Opaque; for "file:/abs/path.db", it's in Path
	pathPart := u.Opaque
	if pathPart == "" {
		pathPart = u.Path
	}
	// Trim leading slash to handle file:/relative.db
	pathPart = strings.TrimPrefix(pathPart, "/")
	if pathPart == "" || strings.HasPrefix(pathPart, ":memory:") {
		return nil
	}

	dir := filepath.Dir(pathPart)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755) //nolint:gosec // directory path is intentionally derived from configured SQLite DSN
}
