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
	"uuid"

	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/italypaleale/francis/components"
	"github.com/italypaleale/francis/components/postgres"
	"github.com/italypaleale/francis/components/sqlite"
	"github.com/italypaleale/francis/host/local"

	sqliteutil "getarcane.dev/arcane/v3/pkg/sqlite"
)

func StorePath(databasePath string) string {
	extension := filepath.Ext(databasePath)
	return strings.TrimSuffix(databasePath, extension) + ".francis" + cmp.Or(extension, ".db")
}

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

func removeOrphanedCancels(ctx context.Context, storeURL string, workflows []*workflow.Workflow) (err error) {
	if len(workflows) == 0 {
		return nil
	}
	provider, err := openProvider(ctx, storeURL)
	if err != nil {
		return fmt.Errorf("open actor storage: %w", err)
	}
	defer func() {
		if closeErr := provider.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close actor provider: %w", closeErr))
		}
	}()
	for _, wf := range workflows {
		actorType := workflow.ActorTypePrefix + wf.Name()
		cancels, listErr := unfinishedCancels(ctx, provider, actorType)
		if listErr != nil {
			return listErr
		}
		if len(cancels) == 0 {
			continue
		}
		started, listErr := startedInstances(ctx, provider, actorType)
		if listErr != nil {
			return listErr
		}
		for _, job := range cancels {
			if _, ok := started[job.ActorID]; ok {
				continue
			}
			deleteErr := provider.DeleteJob(ctx, actorType, job.ActorID, job.JobID, components.DeleteJobReq{})
			if deleteErr != nil && !errors.Is(deleteErr, components.ErrNoJob) {
				return fmt.Errorf("remove orphaned %s cancel job: %w", wf.Name(), deleteErr)
			}
			slog.InfoContext(ctx, "Removed orphaned workflow cancel job", "workflow", wf.Name(), "instanceId", job.ActorID, "jobId", job.JobID)
		}
	}
	return nil
}

func unfinishedCancels(ctx context.Context, provider components.ActorProvider, actorType string) ([]components.JobInfo, error) {
	var cancels []components.JobInfo
	var after components.UUIDCursor
	for {
		page, err := provider.QueryJobs(ctx, components.QueryJobsReq{
			ActorType: actorType,
			After:     after,
			Limit:     components.MaxManagementListLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("list %s jobs: %w", actorType, err)
		}
		for _, job := range page.Jobs {
			if job.Method == "cancel" && job.Status != components.JobStatusCompleted {
				cancels = append(cancels, job)
			}
		}
		if !page.HasMore || len(page.Jobs) == 0 {
			return cancels, nil
		}
		if after, err = uuid.Parse(page.Jobs[len(page.Jobs)-1].JobID); err != nil {
			return nil, fmt.Errorf("page %s jobs: %w", actorType, err)
		}
	}
}

func startedInstances(ctx context.Context, provider components.ActorProvider, actorType string) (map[string]struct{}, error) {
	started := map[string]struct{}{}
	after := ""
	for {
		page, err := provider.ListStates(ctx, components.ListStatesReq{
			ActorType: actorType,
			After:     after,
			Limit:     components.MaxListStatesLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("list %s instances: %w", actorType, err)
		}
		for _, state := range page.States {
			started[state.ActorID] = struct{}{}
		}
		if !page.HasMore || len(page.States) == 0 {
			return started, nil
		}
		after = page.States[len(page.States)-1].ActorID
	}
}

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
