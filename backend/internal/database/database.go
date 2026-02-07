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
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/getarcaneapp/arcane/backend/resources"
)

type DB struct {
	sqlDB    *sql.DB
	pgPool   *pgxpool.Pool
	provider string
}

func Initialize(ctx context.Context, databaseURL string) (*DB, error) {
	db, err := connectDatabase(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dbProvider, err := db.resolveProvider()
	if err != nil {
		return nil, err
	}

	// Run migrations
	if err := migrateDatabase(ctx, db, dbProvider); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Set connection pool settings
	if db.sqlDB == nil {
		return nil, fmt.Errorf("missing sql.DB for database connection")
	}
	db.sqlDB.SetMaxIdleConns(10)
	db.sqlDB.SetMaxOpenConns(100)
	db.sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

func Connect(ctx context.Context, databaseURL string) (*DB, error) {
	db, err := connectDatabase(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	if db.sqlDB == nil {
		return nil, fmt.Errorf("missing sql.DB for database connection")
	}
	db.sqlDB.SetMaxIdleConns(10)
	db.sqlDB.SetMaxOpenConns(100)
	db.sqlDB.SetConnMaxLifetime(time.Hour)
	return db, nil
}

func connectDatabase(ctx context.Context, databaseURL string) (*DB, error) {
	var driverName string
	var dsn string
	var provider string

	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		provider = "sqlite"
		driverName = "sqlite"
		connString, err := parseSqliteConnectionString(databaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse SQLite connection string: %w", err)
		}
		if err := ensureSQLiteDirectory(connString); err != nil {
			return nil, fmt.Errorf("failed to prepare SQLite directory: %w", err)
		}
		dsn = connString
	case strings.HasPrefix(databaseURL, "postgres"):
		provider = "postgres"
		driverName = "pgx"
		dsn = databaseURL
	default:
		return nil, fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	// Retry connection up to 3 times
	var sqlDB *sql.DB
	var err error
	for i := 1; i <= 3; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sqlDB, err = sql.Open(driverName, dsn)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			pingErr := sqlDB.PingContext(pingCtx)
			cancel()
			if pingErr != nil {
				_ = sqlDB.Close()
				err = pingErr
			} else {
				var pgPool *pgxpool.Pool
				if provider == "postgres" {
					pgPool, err = pgxpool.New(ctx, databaseURL)
					if err != nil {
						_ = sqlDB.Close()
						return nil, fmt.Errorf("failed to create pgx pool: %w", err)
					}
				}

				return &DB{
					sqlDB:    sqlDB,
					pgPool:   pgPool,
					provider: provider,
				}, nil
			}
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

func migrateDatabase(ctx context.Context, db *DB, dbProvider string) error {
	if db == nil || db.sqlDB == nil {
		return fmt.Errorf("missing sql.DB for migrations")
	}
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation start", "provider", dbProvider, "operation", "up")
	}

	provider, err := newGooseProvider(dbProvider, db.sqlDB)
	if err != nil {
		return err
	}

	if _, err := provider.GetDBVersion(ctx); err != nil && !errors.Is(err, goose.ErrVersionNotFound) {
		return fmt.Errorf("failed to initialize goose version table: %w", err)
	}

	if err := seedGooseFromSchemaMigrations(ctx, db.sqlDB, dbProvider); err != nil {
		return fmt.Errorf("failed to seed goose migrations: %w", err)
	}

	lastGoodVersion, err := provider.GetDBVersion(ctx)
	if err != nil && !errors.Is(err, goose.ErrVersionNotFound) {
		return fmt.Errorf("failed to read goose db version: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		if _, rollbackErr := provider.DownTo(ctx, lastGoodVersion); rollbackErr != nil {
			return fmt.Errorf("migration failed and rollback failed: %w", errors.Join(err, rollbackErr))
		}
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	if len(results) == 0 {
		slog.Info("Database schema is up to date")
	} else {
		slog.Info("Database migrations completed successfully", "applied", len(results))
	}
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation complete", "provider", dbProvider, "operation", "up", "applied", len(results))
	}

	return nil
}

func newGooseProvider(dbProvider string, sqlDB *sql.DB) (*goose.Provider, error) {
	if sqlDB == nil {
		return nil, fmt.Errorf("sql.DB is nil")
	}

	var dialect goose.Dialect
	var subDir string
	switch dbProvider {
	case "sqlite":
		dialect = goose.DialectSQLite3
		subDir = "migrations/goose_sqlite"
	case "postgres":
		dialect = goose.DialectPostgres
		subDir = "migrations/goose_postgres"
	default:
		return nil, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	fsys, err := fs.Sub(resources.FS, subDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedded goose migration source: %w", err)
	}

	return goose.NewProvider(dialect, sqlDB, fsys)
}

func seedGooseFromSchemaMigrations(ctx context.Context, sqlDB *sql.DB, dbProvider string) error {
	switch dbProvider {
	case "sqlite":
		return seedGooseFromSchemaMigrationsSQLite(ctx, sqlDB)
	case "postgres":
		return seedGooseFromSchemaMigrationsPostgres(ctx, sqlDB)
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func seedGooseFromSchemaMigrationsPostgres(ctx context.Context, sqlDB *sql.DB) error {
	var schemaExists bool
	if err := sqlDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations')").Scan(&schemaExists); err != nil {
		return fmt.Errorf("failed to check schema_migrations table: %w", err)
	}
	if !schemaExists {
		return nil
	}
	if err := ensureGooseVersionTablePostgres(ctx, sqlDB); err != nil {
		return err
	}

	var gooseMax int64
	if err := sqlDB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version").Scan(&gooseMax); err != nil {
		return fmt.Errorf("failed to check goose_db_version table: %w", err)
	}
	if gooseMax > 0 {
		return nil
	}

	query := `
WITH latest AS (
  SELECT COALESCE(MAX(version), 0) AS version
  FROM schema_migrations
  WHERE dirty = false
),
series AS (
  SELECT generate_series(1, (SELECT version FROM latest)) AS version_id
)
INSERT INTO goose_db_version (version_id, is_applied)
SELECT series.version_id, true
FROM series
WHERE NOT EXISTS (
  SELECT 1 FROM goose_db_version g WHERE g.version_id = series.version_id
);`

	if _, err := sqlDB.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to seed goose_db_version from schema_migrations: %w", err)
	}

	return nil
}

func seedGooseFromSchemaMigrationsSQLite(ctx context.Context, sqlDB *sql.DB) error {
	var schemaExists bool
	if err := sqlDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations')").Scan(&schemaExists); err != nil {
		return fmt.Errorf("failed to check schema_migrations table: %w", err)
	}
	if !schemaExists {
		return nil
	}
	if err := ensureGooseVersionTableSQLite(ctx, sqlDB); err != nil {
		return err
	}

	var gooseMax int64
	if err := sqlDB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version").Scan(&gooseMax); err != nil {
		return fmt.Errorf("failed to check goose_db_version table: %w", err)
	}
	if gooseMax > 0 {
		return nil
	}

	query := `
WITH RECURSIVE
  max_version AS (
    SELECT COALESCE(MAX(version), 0) AS v
    FROM schema_migrations
    WHERE dirty = 0
  ),
  seq(x) AS (
    SELECT 1 FROM max_version WHERE v >= 1
    UNION ALL
    SELECT x + 1 FROM seq, max_version WHERE x < v
  )
INSERT INTO goose_db_version (version_id, is_applied)
SELECT x, 1
FROM seq
WHERE NOT EXISTS (
  SELECT 1 FROM goose_db_version g WHERE g.version_id = x
);`

	if _, err := sqlDB.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to seed goose_db_version from schema_migrations: %w", err)
	}

	return nil
}

func ensureGooseVersionTablePostgres(ctx context.Context, sqlDB *sql.DB) error {
	var exists bool
	if err := sqlDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_tables WHERE (current_schema() IS NULL OR schemaname = current_schema()) AND tablename = 'goose_db_version')").Scan(&exists); err != nil {
		return fmt.Errorf("failed to check goose_db_version table: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
		version_id bigint NOT NULL,
		is_applied boolean NOT NULL,
		tstamp timestamp NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("failed to create goose_db_version table: %w", err)
	}
	return nil
}

func ensureGooseVersionTableSQLite(ctx context.Context, sqlDB *sql.DB) error {
	var exists bool
	if err := sqlDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'goose_db_version')").Scan(&exists); err != nil {
		return fmt.Errorf("failed to check goose_db_version table: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("failed to create goose_db_version table: %w", err)
	}
	return nil
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

func (db *DB) SqlDB() *sql.DB {
	if db == nil {
		return nil
	}
	return db.sqlDB
}

func (db *DB) PgPool() *pgxpool.Pool {
	if db == nil {
		return nil
	}
	return db.pgPool
}

func (db *DB) MigrateDown(ctx context.Context, steps int) error {
	if steps <= 0 {
		return fmt.Errorf("steps must be greater than 0")
	}
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation start", "operation", "down", "steps", steps)
	}
	provider, err := db.gooseProvider(ctx)
	if err != nil {
		return err
	}

	for i := 0; i < steps; i++ {
		if _, err := provider.Down(ctx); err != nil {
			if errors.Is(err, goose.ErrNoNextVersion) {
				return nil
			}
			return err
		}
	}
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation complete", "operation", "down", "steps", steps)
	}
	return nil
}

func (db *DB) MigrateDownTo(ctx context.Context, version int64) error {
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation start", "operation", "down-to", "version", version)
	}
	provider, err := db.gooseProvider(ctx)
	if err != nil {
		return err
	}
	_, err = provider.DownTo(ctx, version)
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation complete", "operation", "down-to", "version", version, "error", err)
	}
	return err
}

func (db *DB) MigrateStatus(ctx context.Context) ([]*goose.MigrationStatus, error) {
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation start", "operation", "status")
	}
	provider, err := db.gooseProvider(ctx)
	if err != nil {
		return nil, err
	}
	status, err := provider.Status(ctx)
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation complete", "operation", "status", "entries", len(status), "error", err)
	}
	return status, err
}

func (db *DB) MigrateRedo(ctx context.Context) error {
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation start", "operation", "redo")
	}
	provider, err := db.gooseProvider(ctx)
	if err != nil {
		return err
	}
	if _, err := provider.Down(ctx); err != nil {
		return err
	}
	_, err = provider.UpByOne(ctx)
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database migration operation complete", "operation", "redo", "error", err)
	}
	return err
}

// FindEnvironmentIDByApiKey finds the environment ID associated with the provided API key hash.
func (db *DB) FindEnvironmentIDByApiKey(ctx context.Context, apiKeyHash string) (string, error) {
	if db == nil || db.sqlDB == nil {
		return "", fmt.Errorf("database connection is not initialized")
	}
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "Database query", "driver", db.provider, "op", "query_row", "operation", "FindEnvironmentIDByApiKey", "key_hash_len", len(apiKeyHash))
	}

	provider, err := db.resolveProvider()
	if err != nil {
		return "", err
	}

	query := `
SELECT environments.id
FROM environments
INNER JOIN api_keys ON api_keys.id = environments.api_key_id
WHERE api_keys.key_hash = ?
LIMIT 1;
`
	if provider == "postgres" {
		query = `
SELECT environments.id
FROM environments
INNER JOIN api_keys ON api_keys.id = environments.api_key_id
WHERE api_keys.key_hash = $1
LIMIT 1;
`
	}

	var envID string
	if err := db.sqlDB.QueryRowContext(ctx, query, apiKeyHash).Scan(&envID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", sql.ErrNoRows
		}
		return "", err
	}

	return envID, nil
}

func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	var closeErr error
	if db.pgPool != nil {
		db.pgPool.Close()
	}
	if db.sqlDB != nil {
		closeErr = db.sqlDB.Close()
	}
	return closeErr
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
	return os.MkdirAll(dir, 0o755)
}

func (db *DB) gooseProvider(ctx context.Context) (*goose.Provider, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if db.sqlDB == nil {
		return nil, fmt.Errorf("sql.DB is nil")
	}
	provider, err := db.resolveProvider()
	if err != nil {
		return nil, err
	}
	gooseProvider, err := newGooseProvider(provider, db.sqlDB)
	if err != nil {
		return nil, err
	}
	if _, err := gooseProvider.GetDBVersion(ctx); err != nil && !errors.Is(err, goose.ErrVersionNotFound) {
		return nil, fmt.Errorf("failed to initialize goose version table: %w", err)
	}
	if err := seedGooseFromSchemaMigrations(ctx, db.sqlDB, provider); err != nil {
		return nil, fmt.Errorf("failed to seed goose migrations: %w", err)
	}
	return gooseProvider, nil
}

func (db *DB) resolveProvider() (string, error) {
	if db == nil {
		return "", fmt.Errorf("database is nil")
	}

	if db.provider != "" {
		return db.provider, nil
	}

	if db.pgPool != nil {
		return "postgres", nil
	}

	if db.sqlDB != nil {
		return "sqlite", nil
	}

	return "", fmt.Errorf("unable to determine database provider")
}
