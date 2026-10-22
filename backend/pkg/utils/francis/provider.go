package francis

import (
	"cmp"
	"context"
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

	sqliteutil "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/sqlite"
)

// StorePath names the dedicated Francis SQLite file beside the Arcane database.
func StorePath(databasePath string) string {
	extension := filepath.Ext(databasePath)
	return strings.TrimSuffix(databasePath, extension) + ".francis" + cmp.Or(extension, ".db")
}

// StoreURL resolves Francis storage: a sibling SQLite file with the Arcane SQLite
// settings except foreign_keys, which Francis enforces, or the Postgres URL unchanged.
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
		query := sqliteutil.NormalizeQuery(parsed.Query())
		// The sibling file must be creatable and writable whatever mode opens arcane.db.
		query.Del("mode")
		if !sqliteutil.HasPragma(query, "busy_timeout") {
			query.Add("_pragma", "busy_timeout(10000)")
		}
		// NORMAL stops WAL commits from holding the write lock through an fsync.
		if !sqliteutil.HasPragma(query, "synchronous") {
			query.Add("_pragma", "synchronous(NORMAL)")
		}
		if !sqliteutil.HasPragma(query, "journal_mode") {
			query.Add("_pragma", "journal_mode(WAL)")
		}
		return "file:" + (&url.URL{Path: StorePath(databasePath)}).EscapedPath() + "?" + query.Encode(), nil
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
