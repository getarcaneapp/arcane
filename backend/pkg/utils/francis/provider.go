package francis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dbtypes "github.com/getarcaneapp/arcane/types/v2/database"
	"github.com/italypaleale/francis/components"
	"github.com/italypaleale/francis/components/postgres"
	"github.com/italypaleale/francis/components/sqlite"
	"github.com/italypaleale/francis/host/local"
)

func providerOptionInternal(databaseURL string) (local.HostOption, error) {
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		dsn, err := database.ParseSQLiteConnectionString(databaseURL, dbtypes.SQLiteConnectionOptions{
			IgnoreForeignKeys: true,
			JournalMode:       "WAL",
			BusyTimeout:       2500 * time.Millisecond,
		})
		if err != nil {
			return nil, err
		}
		return local.WithSQLiteProvider(sqlite.SQLiteProviderOptions{ConnectionString: dsn, TablePrefix: tablePrefixInternal}), nil
	case strings.HasPrefix(databaseURL, "postgres"):
		return local.WithPostgresProvider(postgres.PostgresProviderOptions{ConnectionString: databaseURL, TablePrefix: tablePrefixInternal}), nil
	default:
		return nil, errors.New("unsupported actor database URL")
	}
}

// ClearRestoredHosts removes copied live host ownership from an offline restored
// database. Never call this against a running Arcane database.
func ClearRestoredHosts(ctx context.Context, databaseURL string) (err error) {
	cfg := components.NewProviderConfig()
	cfg.HostHealthCheckDeadline = 90 * time.Second
	cfg.MaxHosts = 1
	var provider components.ActorProvider
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		dsn, dsnErr := database.ParseSQLiteConnectionString(databaseURL, dbtypes.SQLiteConnectionOptions{
			IgnoreForeignKeys: true,
			JournalMode:       "WAL",
			BusyTimeout:       2500 * time.Millisecond,
		})
		if dsnErr != nil {
			return dsnErr
		}
		provider, err = sqlite.NewSQLiteProvider(slog.Default(), sqlite.SQLiteProviderOptions{ConnectionString: dsn, TablePrefix: tablePrefixInternal}, cfg)
	case strings.HasPrefix(databaseURL, "postgres"):
		provider, err = postgres.NewPostgresProvider(slog.Default(), postgres.PostgresProviderOptions{ConnectionString: databaseURL, TablePrefix: tablePrefixInternal}, cfg)
	default:
		return errors.New("unsupported restored actor database URL")
	}
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := provider.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close restored actor provider: %w", closeErr))
		}
	}()
	if err = provider.Init(ctx); err != nil {
		return fmt.Errorf("initialize restored actor provider: %w", err)
	}
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
