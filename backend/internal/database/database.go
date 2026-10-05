package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	dbProviderSQLite   = "sqlite"
	dbProviderPostgres = "postgres"
	gooseVersionTable  = "goose_db_version"
	legacyVersionTable = "schema_migrations"

	// Prepared-statement cache bounds. GORM's PrepareStmt cache is a global LRU keyed
	// by SQL text. When PrepareStmtMaxSize/PrepareStmtTTL are left at zero, GORM falls
	// back to math.MaxInt entries with a 24h TTL, i.e. effectively unbounded. Because
	// this codebase emits highly variable SQL (dynamic filter/sort/pagination and
	// GORM's IN (?,?,...) slice expansion, whose placeholder count changes the query
	// text), the cache — and the modernc.org/sqlite compiled statements it retains on
	// the Go heap — grows steadily under normal use. Bounding size and TTL keeps hot
	// queries prepared while evicting the long tail (evicted statements are closed).
	preparedStmtMaxSize = 256
	preparedStmtTTL     = 15 * time.Minute
)

var customGormLogger logger.Interface

func SetGormLogger(l logger.Interface) {
	customGormLogger = l
}

func Initialize(ctx context.Context, databaseURL string, options MigrationOptions) (database *DB, err error) {
	db, err := connectDatabaseInternal(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	// Initialization opens a connection pool before it can fail on migrations or
	// provider detection; release it rather than leaking it into a failed startup.
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

	// Get underlying sql.DB for migrations
	sqlDB, err := db.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	var dbProvider string
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		dbProvider = dbProviderSQLite
	case strings.HasPrefix(databaseURL, "postgres"):
		dbProvider = dbProviderPostgres
	default:
		return nil, fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	if migrateDatabaseErr := migrateDatabaseInternal(ctx, sqlDB, dbProvider, options); migrateDatabaseErr != nil {
		slog.ErrorContext(ctx, "Failed to run migrations", "error", migrateDatabaseErr)
		return nil, fmt.Errorf("failed to run migrations: %w", migrateDatabaseErr)
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

func connectDatabaseInternal(ctx context.Context, databaseURL string) (*DB, error) {
	var dialector gorm.Dialector

	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		if err := sqliteutil.RegisterFunctions(); err != nil {
			return nil, fmt.Errorf("failed to register SQLite functions: %w", err)
		}
		connString, err := ParseSQLiteConnectionString(databaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse SQLite connection string: %w", err)
		}
		if ensureSQLiteDirectoryErr := ensureSQLiteDirectoryInternal(connString); ensureSQLiteDirectoryErr != nil {
			return nil, fmt.Errorf("failed to prepare SQLite directory: %w", ensureSQLiteDirectoryErr)
		}
		dialector = sqlite.Open(connString)
	case strings.HasPrefix(databaseURL, "postgres"):
		dialector = postgres.Open(databaseURL)
	default:
		return nil, fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	// Retry connection up to 3 times
	var db *gorm.DB
	var err error
	for i := 1; i <= 3; i++ {
		if retryCancellationErr := ctx.Err(); retryCancellationErr != nil {
			return nil, retryCancellationErr
		}
		db, err = gorm.Open(dialector, &gorm.Config{
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
			return &DB{db}, nil
		}

		slog.InfoContext(ctx, "Failed to initialize database", "attempt", i)
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

func migrateDatabaseInternal(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions) error {
	requiredVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProvider)
	if err != nil {
		return fmt.Errorf("failed to determine target migration version for %s: %w", dbProvider, err)
	}

	return migrateDatabaseToVersionInternal(ctx, db, dbProvider, options, requiredVersion)
}

func migrateDatabaseToVersionInternal(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions, requiredVersion int64) error {
	if err := adoptLegacyMigrationStateInternal(ctx, db, dbProvider, options); err != nil {
		return err
	}

	provider, err := newGooseProviderInternal(db, dbProvider)
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

		missingVersions, missingEmbeddedDowngradeMigrationsErr := missingEmbeddedDowngradeMigrationsInternal(ctx, db, dbProvider, requiredVersion)
		if missingEmbeddedDowngradeMigrationsErr != nil {
			return missingEmbeddedDowngradeMigrationsErr
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

func newGooseProviderInternal(db *sql.DB, dbProvider string) (*goose.Provider, error) {
	migrationsFS, err := embeddedMigrationFSInternal(dbProvider)
	if err != nil {
		return nil, err
	}

	dialect, err := gooseDialectInternal(dbProvider)
	if err != nil {
		return nil, err
	}

	// Two Arcane processes pointed at the same Postgres (a rolling deploy, or a
	// replica set) would otherwise run migrations concurrently and race on the
	// same DDL. A session-level advisory lock serializes them; SQLite needs no
	// equivalent because it is single-writer by construction.
	var options []goose.ProviderOption
	if dialect == goose.DialectPostgres {
		sessionLocker, newPostgresSessionLockerErr := lock.NewPostgresSessionLocker()
		if newPostgresSessionLockerErr != nil {
			return nil, fmt.Errorf("failed to create Postgres migration session locker: %w", newPostgresSessionLockerErr)
		}
		options = append(options, goose.WithSessionLocker(sessionLocker))
	}

	return goose.NewProvider(dialect, db, migrationsFS, options...)
}

func embeddedMigrationFSInternal(dbProvider string) (fs.FS, error) {
	migrationsFS, err := fs.Sub(resources.FS, "migrations/"+dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded migrations for %s: %w", dbProvider, err)
	}

	return migrationsFS, nil
}

func gooseDialectInternal(dbProvider string) (goose.Dialect, error) {
	switch dbProvider {
	case dbProviderSQLite:
		return goose.DialectSQLite3, nil
	case dbProviderPostgres:
		return goose.DialectPostgres, nil
	default:
		return "", fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func adoptLegacyMigrationStateInternal(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions) error {
	legacyState, ok, err := legacyMigrationStateInternal(ctx, db, dbProvider)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if legacyState.dirty {
		if !options.AllowDowngrade {
			return fmt.Errorf(
				"database schema version %d is dirty in legacy %s table; resolve it manually or set "+
					"ALLOW_DOWNGRADE=true after verifying the database state",
				legacyState.version,
				legacyVersionTable,
			)
		}

		if clearLegacyMigrationDirtyErr := clearLegacyMigrationDirtyInternal(ctx, db, dbProvider, legacyState.version); clearLegacyMigrationDirtyErr != nil {
			return clearLegacyMigrationDirtyErr
		}
		slog.WarnContext(ctx, "Cleared dirty legacy migration state because ALLOW_DOWNGRADE=true", "provider", dbProvider, "version", legacyState.version)
	}

	hasGooseState, err := gooseVersionTableHasAppliedMigrationsInternal(ctx, db, dbProvider)
	if err != nil {
		return err
	}
	if hasGooseState {
		return nil
	}

	versions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
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

	if createGooseVersionTableErr := createGooseVersionTableInternal(ctx, tx, dbProvider); createGooseVersionTableErr != nil {
		return createGooseVersionTableErr
	}

	if clearGooseVersionTableErr := clearGooseVersionTableInternal(ctx, tx, dbProvider); clearGooseVersionTableErr != nil {
		return clearGooseVersionTableErr
	}

	if insertGooseMigrationVersionErr := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, 0); insertGooseMigrationVersionErr != nil {
		return insertGooseMigrationVersionErr
	}
	versionApplied := legacyState.version == 0
	for _, version := range versions {
		if version > legacyState.version {
			break
		}
		if insertMigrationVersionErr := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, version); insertMigrationVersionErr != nil {
			return insertMigrationVersionErr
		}
		if version == legacyState.version {
			versionApplied = true
		}
	}
	if !versionApplied {
		if insertLegacyVersionErr := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, legacyState.version); insertLegacyVersionErr != nil {
			return insertLegacyVersionErr
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("failed to commit legacy migration adoption for %s: %w", dbProvider, commitErr)
	}

	slog.InfoContext(ctx, "Adopted legacy migration state into Goose", "provider", dbProvider, "legacyVersion", legacyState.version)
	return nil
}

type legacyMigrationState struct {
	version int64
	dirty   bool
}

func legacyMigrationStateInternal(ctx context.Context, db *sql.DB, dbProvider string) (legacyMigrationState, bool, error) {
	exists, err := legacyVersionTableExistsInternal(ctx, db, dbProvider)
	if err != nil {
		return legacyMigrationState{}, false, err
	}
	if !exists {
		return legacyMigrationState{}, false, nil
	}

	var state legacyMigrationState
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT version, dirty FROM %s ORDER BY version DESC LIMIT 1", legacyVersionTable)).Scan(&state.version, &state.dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return legacyMigrationState{}, false, nil
	}
	if err != nil {
		return legacyMigrationState{}, false, fmt.Errorf("failed to read legacy migration state for %s: %w", dbProvider, err)
	}

	return state, true, nil
}

func legacyVersionTableExistsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	switch dbProvider {
	case dbProviderSQLite:
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, legacyVersionTable).Scan(&count); err != nil {
			return false, fmt.Errorf("failed to check legacy migration table for sqlite: %w", err)
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, legacyVersionTable).Scan(&exists); err != nil {
			return false, fmt.Errorf("failed to check legacy migration table for postgres: %w", err)
		}
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func clearLegacyMigrationDirtyInternal(ctx context.Context, db *sql.DB, dbProvider string, version int64) error {
	queryFormat := "UPDATE " + legacyVersionTable + " SET dirty = false WHERE version = %s"
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, version)
	if err != nil {
		return err
	}
	if _, execContextErr := db.ExecContext(ctx, query, args...); execContextErr != nil {
		return fmt.Errorf("failed to clear legacy dirty migration state for %s version %d: %w", dbProvider, version, execContextErr)
	}
	return nil
}

func gooseVersionTableHasAppliedMigrationsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	exists, err := gooseVersionTableExistsInternal(ctx, db, dbProvider)
	if err != nil || !exists {
		return false, err
	}

	var version int64
	applied := kit.Ternary(dbProvider == dbProviderPostgres, "true", "1")
	if scanErr := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(MAX(version_id), 0) FROM %s WHERE is_applied = %s", gooseVersionTable, applied)).Scan(&version); scanErr != nil {
		return false, fmt.Errorf("failed to read Goose migration state for %s: %w", dbProvider, scanErr)
	}
	return version > 0, nil
}

func gooseVersionTableExistsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	switch dbProvider {
	case dbProviderSQLite:
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, gooseVersionTable).Scan(&count); err != nil {
			return false, fmt.Errorf("failed to check Goose version table for sqlite: %w", err)
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, gooseVersionTable).Scan(&exists); err != nil {
			return false, fmt.Errorf("failed to check Goose version table for postgres: %w", err)
		}
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

type sqlExecerInternal interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func createGooseVersionTableInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	var query string
	switch dbProvider {
	case dbProviderSQLite:
		query = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	version_id INTEGER NOT NULL,
	is_applied INTEGER NOT NULL,
	tstamp TIMESTAMP DEFAULT (datetime('now'))
)`, gooseVersionTable)
	case dbProviderPostgres:
		query = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
	version_id bigint NOT NULL,
	is_applied boolean NOT NULL,
	tstamp timestamp NOT NULL DEFAULT now()
)`, gooseVersionTable)
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	if _, err := execer.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to create Goose version table for %s: %w", dbProvider, err)
	}
	return nil
}

func clearGooseVersionTableInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	if _, err := execer.ExecContext(ctx, "DELETE FROM "+gooseVersionTable); err != nil {
		return fmt.Errorf("failed to clear Goose version table for %s: %w", dbProvider, err)
	}
	return nil
}

func insertGooseMigrationVersionInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, version int64) error {
	switch dbProvider {
	case dbProviderSQLite:
		if _, err := execer.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (version_id, is_applied) VALUES (?, ?)", gooseVersionTable), version, true); err != nil {
			return fmt.Errorf("failed to insert Goose migration version %d for sqlite: %w", version, err)
		}
	case dbProviderPostgres:
		if _, err := execer.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (version_id, is_applied) VALUES ($1, $2)", gooseVersionTable), version, true); err != nil {
			return fmt.Errorf("failed to insert Goose migration version %d for postgres: %w", version, err)
		}
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
	return nil
}

func sqlWithProviderPlaceholderInternal(dbProvider, queryFormat string, arg any) (string, []any, error) {
	switch dbProvider {
	case dbProviderSQLite:
		return fmt.Sprintf(queryFormat, "?"), []any{arg}, nil
	case dbProviderPostgres:
		return fmt.Sprintf(queryFormat, "$1"), []any{arg}, nil
	default:
		return "", nil, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func getHighestEmbeddedMigrationVersionInternal(dbProvider string) (int64, error) {
	versions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("no embedded migrations found for %s", dbProvider)
	}

	return versions[len(versions)-1], nil
}

func getEmbeddedMigrationVersionsInternal(dbProvider string) ([]int64, error) {
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

	versions := make([]int64, 0, len(versionsMap))
	for version := range versionsMap {
		versions = append(versions, version)
	}
	slices.Sort(versions)

	return versions, nil
}

// missingEmbeddedDowngradeMigrationsInternal returns the applied migration
// versions above requiredVersion that have no matching embedded migration file.
// Goose can only roll back migrations whose .Down SQL is embedded in this
// binary, so any such missing version makes an embedded-only downgrade
// impossible and signals that a restore from backup is required.
func missingEmbeddedDowngradeMigrationsInternal(ctx context.Context, db *sql.DB, dbProvider string, requiredVersion int64) ([]int64, error) {
	embeddedVersions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
	if err != nil {
		return nil, err
	}

	embeddedSet := make(map[int64]struct{}, len(embeddedVersions))
	for _, version := range embeddedVersions {
		embeddedSet[version] = struct{}{}
	}

	queryFormat := "SELECT DISTINCT version_id FROM " + gooseVersionTable +
		" WHERE is_applied = " + kit.Ternary(dbProvider == dbProviderPostgres, "true", "1") +
		" AND version_id > %s ORDER BY version_id"
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, requiredVersion)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read applied migration versions for %s: %w", dbProvider, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var missing []int64
	for rows.Next() {
		var version int64
		if scanErr := rows.Scan(&version); scanErr != nil {
			return nil, fmt.Errorf("failed to scan applied migration version for %s: %w", dbProvider, scanErr)
		}
		if _, ok := embeddedSet[version]; !ok {
			missing = append(missing, version)
		}
	}
	if iterateVersionsErr := rows.Err(); iterateVersionsErr != nil {
		return nil, fmt.Errorf("failed to iterate applied migration versions for %s: %w", dbProvider, iterateVersionsErr)
	}

	return missing, nil
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
		case "_pragma":
			qs["_pragma"] = append(qs["_pragma"], v...)
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
	sqlDB, err := db.SQLDB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (db *DB) SQLDB() (*sql.DB, error) {
	return db.DB.DB()
}

// Create parent directory for file-based SQLite if needed
func ensureSQLiteDirectoryInternal(connString string) error {
	if !strings.HasPrefix(connString, "file:") {
		return nil
	}
	pathPart, err := kit.SQLitePathFromDSN(connString)
	if err != nil {
		return fmt.Errorf("failed to parse SQLite DSN: %w", err)
	}
	if pathPart == "" || strings.HasPrefix(pathPart, ":memory:") {
		return nil
	}

	dir := filepath.Dir(pathPart)
	if dir == "" || dir == "." {
		return nil
	}
	// os.* rather than acfs: this creates the sqlite data directory itself,
	// which has to exist before any acfs root could be opened on it.
	return os.MkdirAll(dir, 0o755)
}
