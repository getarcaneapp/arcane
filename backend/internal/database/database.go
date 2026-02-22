package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	postgresMigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/getarcaneapp/arcane/backend/resources"
)

type DB struct {
	*gorm.DB
}

var customGormLogger logger.Interface

func SetGormLogger(l logger.Interface) {
	customGormLogger = l
}

func Initialize(ctx context.Context, databaseURL string) (*DB, error) {
	db, err := connectDatabase(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Get underlying sql.DB for migrations
	sqlDB, err := db.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// Determine database provider for migrations
	var dbProvider string
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		dbProvider = "sqlite"
	case strings.HasPrefix(databaseURL, "postgres"):
		dbProvider = "postgres"
	default:
		return nil, fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	// Choose the correct driver for migrations
	var driver database.Driver
	switch dbProvider {
	case "sqlite":
		driver, err = sqliteMigrate.WithInstance(sqlDB, &sqliteMigrate.Config{})
	case "postgres":
		driver, err = postgresMigrate.WithInstance(sqlDB, &postgresMigrate.Config{})
	default:
		return nil, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create migration driver: %w", err)
	}

	// Run migrations
	if err := migrateDatabase(driver, dbProvider); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Set connection pool settings
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(3 * time.Minute)

	return db, nil
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

func migrateDatabase(driver database.Driver, dbProvider string) error {
	source, err := iofs.New(resources.FS, "migrations/"+dbProvider)
	if err != nil {
		return fmt.Errorf("failed to create embedded migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "arcane", driver)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		version, dirty, versionErr := m.Version()
		if versionErr != nil {
			slog.Info("Database schema is up to date")
		} else {
			slog.Info("Database schema is up to date", "migrationVersion", version, "dirty", dirty)
		}
	} else {
		slog.Info("Database migrations completed successfully")
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
