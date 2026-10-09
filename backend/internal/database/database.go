package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	sqliteutil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/sqlite"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
)

type DB struct {
	*gorm.DB
}

type MigrationOptions struct {
	AllowDowngrade bool
}

const (
	dbProviderSQLite             = "sqlite"
	dbProviderPostgres           = "postgres"
	gooseVersionTable            = "goose_db_version"
	legacyVersionTable           = "schema_migrations"
	latestMigrationVersion int64 = -1

	// Bound prepared statements so variable SQL does not retain an unbounded cache.
	preparedStmtMaxSize = 256
	preparedStmtTTL     = 15 * time.Minute
)

var customGormLogger logger.Interface

func SetGormLogger(l logger.Interface) {
	customGormLogger = l
}

func Initialize(ctx context.Context, databaseURL string, options MigrationOptions) (database *DB, err error) {
	var dialector gorm.Dialector
	var dbProvider string
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		dbProvider = dbProviderSQLite
		if err = sqliteutil.RegisterFunctions(); err != nil {
			err = fmt.Errorf("failed to register SQLite functions: %w", err)
			break
		}
		var connString string
		connString, err = ParseSQLiteConnectionString(databaseURL)
		if err != nil {
			err = fmt.Errorf("failed to parse SQLite connection string: %w", err)
			break
		}
		var path string
		path, err = kit.SQLitePathFromDSN(connString)
		if err != nil {
			err = fmt.Errorf("failed to prepare SQLite directory: failed to parse SQLite DSN: %w", err)
			break
		}
		dir := filepath.Dir(path)
		if path != "" && !strings.HasPrefix(path, ":memory:") && dir != "" && dir != "." {
			if err = os.MkdirAll(dir, 0o755); err != nil {
				err = fmt.Errorf("failed to prepare SQLite directory: %w", err)
				break
			}
		}
		dialector = sqlite.Open(connString)
	case strings.HasPrefix(databaseURL, "postgres"):
		dbProvider = dbProviderPostgres
		dialector = postgres.Open(databaseURL)
	default:
		err = fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	var gormDB *gorm.DB
	for attempt := 1; attempt <= 3; attempt++ {
		if err = ctx.Err(); err != nil {
			return nil, fmt.Errorf("failed to connect to database: %w", err)
		}
		gormDB, err = gorm.Open(dialector, &gorm.Config{
			Logger: customGormLogger,
			NowFunc: func() time.Time {
				return time.Now().UTC()
			},
			PrepareStmt:                      true,
			PrepareStmtMaxSize:               preparedStmtMaxSize,
			PrepareStmtTTL:                   preparedStmtTTL,
			IgnoreRelationshipsWhenMigrating: true,
		})
		if err == nil {
			break
		}
		slog.InfoContext(ctx, "Failed to initialize database", "attempt", attempt)
		if attempt < 3 {
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return nil, fmt.Errorf("failed to connect to database: %w", ctx.Err())
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	db := &DB{DB: gormDB}
	// Close the pool if initialization fails after opening it.
	defer func() {
		if err == nil {
			return
		}
		if closeErr := db.Close(); closeErr != nil {
			slog.WarnContext(ctx, "Failed to close database after initialization failure", "error", closeErr)
		}
	}()
	if cancellationErr := ctx.Err(); cancellationErr != nil {
		return nil, cancellationErr
	}

	sqlDB, err := db.SQLDB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}
	if migrationErr := migrateDatabase(ctx, sqlDB, dbProvider, options, latestMigrationVersion); migrationErr != nil {
		slog.ErrorContext(ctx, "Failed to run migrations", "error", migrationErr)
		return nil, fmt.Errorf("failed to run migrations: %w", migrationErr)
	}

	sqlDB.SetMaxIdleConns(kit.Ternary(dbProvider == dbProviderPostgres, 15, 5))
	sqlDB.SetMaxOpenConns(kit.Ternary(dbProvider == dbProviderPostgres, 50, 20))
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(3 * time.Minute)
	return db, nil
}

// latestMigrationVersion selects the latest embedded migration.
func migrateDatabase(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions, requiredVersion int64) error {
	if requiredVersion == latestMigrationVersion {
		versions, err := getEmbeddedMigrationVersions(dbProvider)
		if err != nil {
			return fmt.Errorf("failed to determine target migration version for %s: %w", dbProvider, err)
		}
		if len(versions) == 0 {
			return fmt.Errorf("failed to determine target migration version for %s: no embedded migrations found for %s", dbProvider, dbProvider)
		}
		requiredVersion = versions[len(versions)-1]
	}

	if err := adoptLegacyMigrationState(ctx, db, dbProvider, options); err != nil {
		return err
	}

	migrationsFS, err := fs.Sub(resources.FS, "migrations/"+dbProvider)
	if err != nil {
		return fmt.Errorf("failed to create goose provider for %s: failed to load embedded migrations for %s: %w", dbProvider, dbProvider, err)
	}
	var dialect goose.Dialect
	switch dbProvider {
	case dbProviderSQLite:
		dialect = goose.DialectSQLite3
	case dbProviderPostgres:
		dialect = goose.DialectPostgres
	default:
		return fmt.Errorf("failed to create goose provider for %s: unsupported database provider: %s", dbProvider, dbProvider)
	}
	// Serialize Postgres migrations across processes. SQLite already allows one writer.
	providerOptions := []goose.ProviderOption{}
	if dialect == goose.DialectPostgres {
		sessionLocker, lockerErr := lock.NewPostgresSessionLocker()
		if lockerErr != nil {
			return fmt.Errorf("failed to create goose provider for %s: failed to create Postgres migration session locker: %w", dbProvider, lockerErr)
		}
		providerOptions = append(providerOptions, goose.WithSessionLocker(sessionLocker))
	}
	provider, err := goose.NewProvider(dialect, db, migrationsFS, providerOptions...)
	if err != nil {
		return fmt.Errorf("failed to create goose provider for %s: %w", dbProvider, err)
	}

	currentVersion, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to determine current migration version for %s: %w", dbProvider, err)
	}

	slog.InfoContext(ctx, "Resolved database migration state", "provider", dbProvider, "currentVersion", currentVersion, "requiredVersion", requiredVersion)

	if currentVersion > requiredVersion {
		if !options.AllowDowngrade {
			return fmt.Errorf(
				"database schema version %d is newer than this Arcane binary supports (target %d for %s); downgrade "+
					"requires ALLOW_DOWNGRADE=true and a database backup before startup",
				currentVersion,
				requiredVersion,
				dbProvider,
			)
		}

		embeddedVersions, versionsErr := getEmbeddedMigrationVersions(dbProvider)
		if versionsErr != nil {
			return versionsErr
		}
		applied := kit.Ternary(dbProvider == dbProviderPostgres, "true", "1")
		placeholder := kit.Ternary(dbProvider == dbProviderPostgres, "$1", "?")
		query := "SELECT DISTINCT version_id FROM " + gooseVersionTable +
			" WHERE is_applied = " + applied + " AND version_id > " + placeholder + " ORDER BY version_id"
		rows, queryErr := db.QueryContext(ctx, query, requiredVersion)
		if queryErr != nil {
			return fmt.Errorf("failed to read applied migration versions for %s: %w", dbProvider, queryErr)
		}
		defer func() {
			_ = rows.Close()
		}()
		missingVersions := []int64{}
		for rows.Next() {
			var version int64
			if scanErr := rows.Scan(&version); scanErr != nil {
				return fmt.Errorf("failed to scan applied migration version for %s: %w", dbProvider, scanErr)
			}
			if !slices.Contains(embeddedVersions, version) {
				missingVersions = append(missingVersions, version)
			}
		}
		if iterateErr := rows.Err(); iterateErr != nil {
			return fmt.Errorf("failed to iterate applied migration versions for %s: %w", dbProvider, iterateErr)
		}
		if len(missingVersions) > 0 {
			return fmt.Errorf("cannot downgrade database from version %d to %d for %s: embedded Goose migrations are missing for "+
				"applied version(s) %v, so the rollback SQL is unavailable in this Arcane binary; "+
				"ALLOW_DOWNGRADE=true is not sufficient, restore the database from a backup taken before the newer "+
				"schema was applied",
				currentVersion,
				requiredVersion,
				dbProvider,
				missingVersions,
			)
		}

		if _, downToErr := provider.DownTo(ctx, requiredVersion); downToErr != nil {
			return fmt.Errorf("failed to downgrade database from version %d to %d for %s using embedded Goose migrations: %w", currentVersion, requiredVersion, dbProvider, downToErr)
		}

		slog.InfoContext(ctx, "Database downgrade completed successfully", "provider", dbProvider, "fromVersion", currentVersion, "toVersion", requiredVersion)
		return nil
	}

	if currentVersion == requiredVersion {
		slog.InfoContext(ctx, "Database schema is up to date", "provider", dbProvider, "migrationVersion", currentVersion)
		return nil
	}

	if _, upToErr := provider.UpTo(ctx, requiredVersion); upToErr != nil {
		return fmt.Errorf("failed to apply embedded Goose migrations for %s: %w", dbProvider, upToErr)
	}

	slog.InfoContext(ctx, "Database migrations completed successfully", "provider", dbProvider, "targetVersion", requiredVersion)
	return nil
}

// Remove in v3
func adoptLegacyMigrationState(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions) error {
	if dbProvider != dbProviderSQLite && dbProvider != dbProviderPostgres {
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
	tableQuery := kit.Ternary(dbProvider == dbProviderPostgres,
		"SELECT to_regclass($1) IS NOT NULL",
		"SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)",
	)
	var legacyVersion int64
	var dirty bool
	for _, table := range []string{legacyVersionTable, gooseVersionTable} {
		var exists bool
		if err := db.QueryRowContext(ctx, tableQuery, table).Scan(&exists); err != nil {
			label := kit.Ternary(table == legacyVersionTable, "legacy migration table", "Goose version table")
			return fmt.Errorf("failed to check %s for %s: %w", label, dbProvider, err)
		}
		if table == gooseVersionTable {
			if !exists {
				break
			}
			var version int64
			applied := kit.Ternary(dbProvider == dbProviderPostgres, "true", "1")
			query := fmt.Sprintf("SELECT COALESCE(MAX(version_id), 0) FROM %s WHERE is_applied = %s", table, applied)
			if err := db.QueryRowContext(ctx, query).Scan(&version); err != nil {
				return fmt.Errorf("failed to read Goose migration state for %s: %w", dbProvider, err)
			}
			if version > 0 {
				return nil
			}
			continue
		}
		if !exists {
			return nil
		}
		query := "SELECT version, dirty FROM " + table + " ORDER BY version DESC LIMIT 1"
		err := db.QueryRowContext(ctx, query).Scan(&legacyVersion, &dirty)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read legacy migration state for %s: %w", dbProvider, err)
		}
		if dirty {
			if !options.AllowDowngrade {
				return fmt.Errorf(
					"database schema version %d is dirty in legacy %s table; resolve it manually or set "+
						"ALLOW_DOWNGRADE=true after verifying the database state",
					legacyVersion,
					legacyVersionTable,
				)
			}
			placeholder := kit.Ternary(dbProvider == dbProviderPostgres, "$1", "?")
			query = "UPDATE " + table + " SET dirty = false WHERE version = " + placeholder
			if _, err = db.ExecContext(ctx, query, legacyVersion); err != nil {
				return fmt.Errorf("failed to clear legacy dirty migration state for %s version %d: %w", dbProvider, legacyVersion, err)
			}
			slog.WarnContext(ctx, "Cleared dirty legacy migration state because ALLOW_DOWNGRADE=true", "provider", dbProvider, "version", legacyVersion)
		}
	}

	versions, err := getEmbeddedMigrationVersions(dbProvider)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start legacy migration adoption transaction for %s: %w", dbProvider, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	versionDDL := fmt.Sprintf(kit.Ternary(dbProvider == dbProviderPostgres,
		`CREATE TABLE IF NOT EXISTS %s (
	id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
	version_id bigint NOT NULL,
	is_applied boolean NOT NULL,
	tstamp timestamp NOT NULL DEFAULT now()
)`,
		`CREATE TABLE IF NOT EXISTS %s (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	version_id INTEGER NOT NULL,
	is_applied INTEGER NOT NULL,
	tstamp TIMESTAMP DEFAULT (datetime('now'))
)`,
	), gooseVersionTable)
	if _, createErr := tx.ExecContext(ctx, versionDDL); createErr != nil {
		return fmt.Errorf("failed to create Goose version table for %s: %w", dbProvider, createErr)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM "+gooseVersionTable); err != nil {
		return fmt.Errorf("failed to clear Goose version table for %s: %w", dbProvider, err)
	}

	if end := slices.IndexFunc(versions, func(version int64) bool { return version > legacyVersion }); end >= 0 {
		versions = versions[:end]
	}
	appliedVersions := append([]int64{0}, versions...)
	if !slices.Contains(appliedVersions, legacyVersion) {
		appliedVersions = append(appliedVersions, legacyVersion)
	}
	first := kit.Ternary(dbProvider == dbProviderPostgres, "$1", "?")
	second := kit.Ternary(dbProvider == dbProviderPostgres, "$2", "?")
	insertQuery := fmt.Sprintf("INSERT INTO %s (version_id, is_applied) VALUES (%s, %s)", gooseVersionTable, first, second)
	for _, version := range appliedVersions {
		if _, insertErr := tx.ExecContext(ctx, insertQuery, version, true); insertErr != nil {
			return fmt.Errorf("failed to insert Goose migration version %d for %s: %w", version, dbProvider, insertErr)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit legacy migration adoption for %s: %w", dbProvider, err)
	}
	slog.InfoContext(ctx, "Adopted legacy migration state into Goose", "provider", dbProvider, "legacyVersion", legacyVersion)
	return nil
}

func getEmbeddedMigrationVersions(dbProvider string) ([]int64, error) {
	entries, err := resources.FS.ReadDir("migrations/" + dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded migrations for %s: %w", dbProvider, err)
	}

	versionsMap := make(map[int64]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		versionText, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			continue
		}
		version, parseErr := strconv.ParseInt(versionText, 10, 64)
		if parseErr != nil {
			continue
		}

		versionsMap[version] = struct{}{}
	}

	return slices.Sorted(maps.Keys(versionsMap)), nil
}

func ParseSQLiteConnectionString(connString string) (string, error) {
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
			// Foreign-key enforcement is required on every pooled connection.
			continue
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
		case "_pragma":
			for _, pragma := range v {
				name := strings.TrimSpace(pragma)
				if end := strings.IndexAny(name, "=( \t\r\n"); end >= 0 {
					name = name[:end]
				}
				if _, unqualified, found := strings.Cut(name, "."); found {
					name = unqualified
				}
				if strings.EqualFold(strings.Trim(name, "\"'`[]"), "foreign_keys") {
					continue
				}
				qs.Add("_pragma", pragma)
			}
		default:
			qs[k] = v
		}
	}

	qs.Add("_pragma", "foreign_keys(1)")
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
	sqlDB, err := db.SQLDB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (db *DB) SQLDB() (*sql.DB, error) {
	return db.DB.DB()
}
