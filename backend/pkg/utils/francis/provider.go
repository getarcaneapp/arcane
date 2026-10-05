package francis

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/italypaleale/francis/components"
	"github.com/italypaleale/francis/components/postgres"
	"github.com/italypaleale/francis/components/sqlite"
	"github.com/italypaleale/francis/host/local"
)

// legacyDrops remove the Francis schema that lived in the Arcane SQLite database
// before v2.16. That schema never changes again, so the list is final.
// Remove this in v3.0.0
var legacyDrops = []string{
	"DROP VIEW IF EXISTS " + TablePrefix + "_host_active_actor_count",
	"DROP TABLE IF EXISTS " + TablePrefix + "_host_actor_types",
	"DROP TABLE IF EXISTS " + TablePrefix + "_active_actors",
	"DROP TABLE IF EXISTS " + TablePrefix + "_consumed_join_tokens",
	"DROP TABLE IF EXISTS " + TablePrefix + "_hosts",
	"DROP TABLE IF EXISTS " + TablePrefix + "_actor_state",
	"DROP TABLE IF EXISTS " + TablePrefix + "_alarms",
	"DROP TABLE IF EXISTS " + TablePrefix + "_cluster_config",
	"DROP TABLE IF EXISTS " + TablePrefix + "_terminal_jobs",
	"DROP TABLE IF EXISTS " + TablePrefix + "_dead_jobs",
	"DROP TABLE IF EXISTS " + TablePrefix + "_metadata",
}

// StorePath names the dedicated Francis SQLite file beside the Arcane database.
func StorePath(databasePath string) string {
	extension := filepath.Ext(databasePath)
	return strings.TrimSuffix(databasePath, extension) + ".francis" + cmp.Or(extension, ".db")
}

// StoreURL resolves Francis storage: a sibling SQLite file without application
// pragmas, or the Postgres URL unchanged.
func StoreURL(databaseURL string) (string, error) {
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			return "", fmt.Errorf("parse actor database URL: %w", err)
		}
		databasePath := parsed.Path
		if parsed.Opaque != "" {
			if databasePath, err = url.PathUnescape(parsed.Opaque); err != nil {
				return "", fmt.Errorf("parse actor database URL: %w", err)
			}
		}
		if databasePath == "" || strings.HasPrefix(databasePath, ":memory:") || parsed.Query().Get("mode") == "memory" {
			return "", errors.New("actor storage requires a file-backed SQLite database")
		}
		return "file:" + (&url.URL{Path: StorePath(databasePath)}).EscapedPath(), nil
	case strings.HasPrefix(databaseURL, "postgres"):
		return databaseURL, nil
	default:
		return "", errors.New("unsupported actor database URL")
	}
}

func providerOption(storeURL string) local.HostOption {
	if strings.HasPrefix(storeURL, "file:") {
		return local.WithSQLiteProvider(sqlite.SQLiteProviderOptions{ConnectionString: storeURL, TablePrefix: TablePrefix})
	}
	return local.WithPostgresProvider(postgres.PostgresProviderOptions{ConnectionString: storeURL, TablePrefix: TablePrefix})
}

// openProvider opens an offline Francis provider on storeURL. Close it when done.
func openProvider(ctx context.Context, storeURL string) (components.ActorProvider, error) {
	cfg := components.NewProviderConfig()
	cfg.HostHealthCheckDeadline = 90 * time.Second
	cfg.MaxHosts = 1
	var provider components.ActorProvider
	var err error
	if strings.HasPrefix(storeURL, "file:") {
		provider, err = sqlite.NewSQLiteProvider(slog.Default(), sqlite.SQLiteProviderOptions{ConnectionString: storeURL, TablePrefix: TablePrefix}, cfg)
	} else {
		provider, err = postgres.NewPostgresProvider(slog.Default(), postgres.PostgresProviderOptions{ConnectionString: storeURL, TablePrefix: TablePrefix}, cfg)
	}
	if err != nil {
		return nil, err
	}
	if err = provider.Init(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("initialize actor provider: %w", err), provider.Close())
	}
	return provider, nil
}

// ClearRestoredHosts removes copied live host ownership from an offline restored
// database. Never call this against a running Arcane database.
func ClearRestoredHosts(ctx context.Context, databaseURL string) (err error) {
	storeURL, err := StoreURL(databaseURL)
	if err != nil {
		return err
	}
	provider, err := openProvider(ctx, storeURL)
	if err != nil {
		return fmt.Errorf("open restored actor storage: %w", err)
	}
	defer func() {
		if closeErr := provider.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close restored actor provider: %w", closeErr))
		}
	}()
	hosts, err := provider.ListHosts(ctx)
	if err != nil {
		return fmt.Errorf("list restored actor hosts: %w", err)
	}
	for _, host := range hosts {
		err = provider.UnregisterHost(ctx, host.HostID, components.UnregisterHostOpts{})
		if err != nil && !errors.Is(err, components.ErrHostUnregistered) {
			return fmt.Errorf("remove restored actor host: %w", err)
		}
	}
	// Expired registrations are excluded by ListHosts and pruned by RegisterHost.
	return nil
}

// MigrateLegacyStore moves Francis state kept inside the Arcane SQLite database
// into its dedicated file, then drops the old tables. Reruns until the drop succeeds.
func MigrateLegacyStore(ctx context.Context, mainDB *sql.DB, databaseURL string) (err error) {
	if !strings.HasPrefix(databaseURL, "file:") {
		return nil
	}
	var legacy int
	var journalMode string
	err = errors.Join(
		mainDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", TablePrefix+"_metadata").Scan(&legacy),
		mainDB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode),
	)
	if err != nil || legacy == 0 {
		return err
	}
	storeURL, err := StoreURL(databaseURL)
	if err != nil {
		return err
	}
	// Francis needs its own connection with foreign keys on; keep the Arcane journal mode.
	legacyURL, _, _ := strings.Cut(databaseURL, "?")
	source, err := openProvider(ctx, legacyURL+"?_pragma="+url.QueryEscape("journal_mode("+journalMode+")"))
	if err != nil {
		return fmt.Errorf("open legacy actor storage: %w", err)
	}
	var snapshot bytes.Buffer
	err = errors.Join(source.Backup(ctx, &snapshot), source.Close())
	if err != nil {
		return fmt.Errorf("export legacy actor storage: %w", err)
	}
	target, err := openProvider(ctx, storeURL)
	if err != nil {
		return fmt.Errorf("open actor storage: %w", err)
	}
	err = errors.Join(target.Restore(ctx, &snapshot), target.Close())
	if err != nil {
		return fmt.Errorf("import legacy actor storage: %w", err)
	}
	tx, err := mainDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy actor storage cleanup: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back legacy actor storage cleanup: %w", rollbackErr))
		}
	}()
	// Keep the metadata marker and every source table intact until cleanup commits.
	for _, drop := range legacyDrops {
		if _, err = tx.ExecContext(ctx, drop); err != nil {
			return fmt.Errorf("drop legacy actor storage: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy actor storage cleanup: %w", err)
	}
	slog.InfoContext(ctx, "Migrated actor state to its dedicated database", "path", strings.TrimPrefix(storeURL, "file:"))
	return nil
}
