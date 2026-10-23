package database

import (
	"cmp"
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
	"gorm.io/plugin/opentelemetry/metrics"

	sqliteutil "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/sqlite"
	"github.com/getarcaneapp/arcane/backend/v3/resources"
)

type DB struct {
	*gorm.DB
	reader *sql.DB
}

type MigrationOptions struct {
	AllowDowngrade bool
}

const (
	dbProviderSQLite             = "sqlite"
	dbProviderPostgres           = "postgres"
	gooseVersionTable            = "goose_db_version"
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
	var readDSN string
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
		readDSN = sqliteReadDSN(connString)
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
	connectStart := time.Now()
	for attempt := 1; ; attempt++ {
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
			SkipDefaultTransaction:           true,
			IgnoreRelationshipsWhenMigrating: true,
		})
		if err == nil {
			break
		}
		wait := min(time.Second<<min(attempt-1, 4), 15*time.Second)
		if time.Since(connectStart)+wait > time.Minute {
			err = fmt.Errorf("gave up after %d attempts: %w", attempt, err)
			break
		}
		slog.InfoContext(ctx, "Failed to initialize database", "attempt", attempt, "retryIn", wait, "error", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, fmt.Errorf("failed to connect to database: %w", ctx.Err())
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
	// Statement spans and pool metrics are no-ops until telemetry is enabled.
	cb := gormDB.Callback()
	err = errors.Join(
		cb.Create().Before("gorm:create").Register("arcane:otel_before_create", startStatementSpan),
		cb.Create().After("gorm:create").Register("arcane:otel_after_create", endStatementSpan),
		cb.Query().Before("gorm:query").Register("arcane:otel_before_query", startStatementSpan),
		cb.Query().After("gorm:query").Register("arcane:otel_after_query", endStatementSpan),
		cb.Update().Before("gorm:update").Register("arcane:otel_before_update", startStatementSpan),
		cb.Update().After("gorm:update").Register("arcane:otel_after_update", endStatementSpan),
		cb.Delete().Before("gorm:delete").Register("arcane:otel_before_delete", startStatementSpan),
		cb.Delete().After("gorm:delete").Register("arcane:otel_after_delete", endStatementSpan),
		cb.Row().Before("gorm:row").Register("arcane:otel_before_row", startStatementSpan),
		cb.Row().After("gorm:row").Register("arcane:otel_after_row", endStatementSpan),
		cb.Raw().Before("gorm:raw").Register("arcane:otel_before_raw", startStatementSpan),
		cb.Raw().After("gorm:raw").Register("arcane:otel_after_raw", endStatementSpan),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register database instrumentation: %w", err)
	}
	if dbProvider == dbProviderPostgres {
		sqlDB.SetMaxIdleConns(15)
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetConnMaxLifetime(5 * time.Minute)
		sqlDB.SetConnMaxIdleTime(3 * time.Minute)
	} else {
		sqlDB.SetMaxIdleConns(kit.Ternary(readDSN == "", 10, sqliteWriteConns))
		sqlDB.SetMaxOpenConns(kit.Ternary(readDSN == "", 10, sqliteWriteConns))
		sqlDB.SetConnMaxLifetime(0)
		sqlDB.SetConnMaxIdleTime(0)
	}
	metrics.ReportDBStatsMetrics(sqlDB)
	if migrationErr := migrateDatabase(ctx, sqlDB, dbProvider, options, latestMigrationVersion); migrationErr != nil {
		slog.ErrorContext(ctx, "Failed to run migrations", "error", migrationErr)
		return nil, fmt.Errorf("failed to run migrations: %w", migrationErr)
	}
	if readDSN == "" {
		return db, nil
	}
	reader, err := attachSQLiteReadPool(gormDB, sqlDB, readDSN)
	if err != nil {
		return nil, err
	}
	return &DB{
		DB:     gormDB,
		reader: reader,
	}, nil
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
					"requires ARCANE_ALLOW_DOWNGRADE=true and a database backup before startup",
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
				"ARCANE_ALLOW_DOWNGRADE=true is not sufficient, restore the database from a backup taken before the newer "+
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

	qs := sqliteutil.NormalizeQuery(connStringUrl.Query())
	// Foreign-key enforcement is required on every pooled connection.
	qs.Add("_pragma", "foreign_keys(1)")
	if !sqliteutil.HasPragma(qs, "busy_timeout") {
		qs.Add("_pragma", "busy_timeout(10000)")
	}
	if !slices.Contains([]string{"ro", "memory"}, qs.Get("mode")) && !strings.HasPrefix(cmp.Or(connStringUrl.Opaque, connStringUrl.Path), ":memory:") {
		if !sqliteutil.HasPragma(qs, "journal_mode") {
			qs.Add("_pragma", "journal_mode(WAL)")
		}
		if !sqliteutil.HasPragma(qs, "synchronous") {
			qs.Add("_pragma", "synchronous(NORMAL)")
		}
		if !sqliteutil.HasPragma(qs, "cache_size") {
			qs.Add("_pragma", "cache_size(-8000)")
		}
		if !sqliteutil.HasPragma(qs, "temp_store") {
			qs.Add("_pragma", "temp_store(MEMORY)")
		}
		if qs.Get("_txlock") == "" {
			qs.Set("_txlock", "immediate")
		}
	}
	return (&url.URL{
		Scheme:   connStringUrl.Scheme,
		Opaque:   connStringUrl.Opaque,
		User:     connStringUrl.User,
		Host:     connStringUrl.Host,
		Path:     connStringUrl.Path,
		RawPath:  connStringUrl.RawPath,
		RawQuery: qs.Encode(),
		Fragment: connStringUrl.Fragment,
	}).String(), nil
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
	if db.reader == nil {
		return sqlDB.Close()
	}
	return errors.Join(db.reader.Close(), sqlDB.Close())
}

func (db *DB) SQLDB() (*sql.DB, error) {
	return db.DB.DB()
}
