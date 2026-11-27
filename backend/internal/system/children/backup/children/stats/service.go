package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	mobyClient "github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"

	kit "kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/backup"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type (
	Dependencies struct {
		Engine             *backup.Engine
		Docker             *dockerInternal.DockerClientService
		Repository         *backup.VolumeRepository
		S3Destinations     *s3.S3DestinationService
		Settings           *settings.SettingsService
		RecoveryKeys       *backup.RecoveryKeyStore
		SystemRepositories func(ctx context.Context, dockerClient *mobyClient.Client) ([]backup.Repository, error)
		History            func(ctx context.Context, params pagination.QueryParams) ([]types.SystemVolumeBackupHistoryEntry, pagination.Response, error)
	}

	Service struct {
		deps Dependencies
	}

	target struct {
		password   string
		repository backup.Repository
		volume     bool
	}
)

const refreshConcurrency = 4

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) Get(ctx context.Context) (types.BackupStorageStats, error) {
	stored, err := s.deps.Engine.Stats(ctx)
	if err != nil {
		return types.BackupStorageStats{}, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	instanceID := strings.TrimSpace(s.deps.Settings.GetSettingsConfig().InstanceID.Value)
	repositories := make([]types.BackupRepositoryStats, 0, len(stored))
	for _, entry := range stored {
		kind, rest, _ := strings.Cut(entry.ID, ":")
		destination, rest, _ := strings.Cut(rest, ":")
		destinationID, repositoryInstanceID, _ := strings.Cut(rest, ":")
		if (destinationID != "" && destinations[destinationID].ID == "") || (repositoryInstanceID != "" && repositoryInstanceID != instanceID) {
			continue
		}
		repositories = append(repositories, types.BackupRepositoryStats{ //exhaustruct:enforce
			UpdatedAt:         kit.FromPtr(entry.UpdatedAt),
			ID:                entry.ID,
			Kind:              types.BackupRepositoryKind(kind),
			Destination:       types.SystemBackupDestination(destination),
			S3DestinationID:   destinationID,
			S3DestinationName: destinations[destinationID].Name,
			SnapshotCount:     entry.SnapshotCount,
			StoredBytes:       entry.StoredBytes,
			RawBytes:          entry.RawBytes,
			LogicalBytes:      entry.LogicalBytes,
		})
	}
	latest, _, err := s.deps.History(ctx, pagination.QueryParams{
		SortParams: pagination.SortParams{Sort: "createdAt", Order: pagination.SortDesc},
		Params:     pagination.Params{Start: 0, Limit: 1},
		Filters:    map[string]string{"status": types.SystemBackupStatusSucceeded},
	})
	if err != nil {
		return types.BackupStorageStats{}, fmt.Errorf("failed to load the latest backup: %w", err)
	}
	var lastBackup *types.SystemVolumeBackupHistoryEntry
	if len(latest) > 0 {
		latest[0].S3DestinationName = destinations[latest[0].S3DestinationID].Name
		lastBackup = &latest[0]
	}
	return types.BackupStorageStats{LastBackup: lastBackup, Repositories: repositories}, nil
}

func (s *Service) Aggregate(ctx context.Context) (types.ApiResponse[types.BackupStorageStats], error) {
	return httpx.CollectEnvironmentData(ctx, nil, mergeEnvironments)
}

func (s *Service) Refresh(ctx context.Context) (types.BackupStorageStats, error) {
	if !s.deps.Docker.LocalDockerEnabled() {
		return s.Get(ctx)
	}
	refreshCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultAgentProxy)
	defer cancel()
	dockerClient, err := s.deps.Docker.GetClient(refreshCtx)
	if err != nil {
		return types.BackupStorageStats{}, err
	}
	targets, err := s.targets(refreshCtx, dockerClient)
	if err != nil {
		return types.BackupStorageStats{}, err
	}
	var (
		mu       sync.Mutex
		failures []error
		group    errgroup.Group
	)
	group.SetLimit(refreshConcurrency)
	for _, entry := range targets {
		group.Go(func() error {
			refreshErr := s.refresh(refreshCtx, dockerClient, entry)
			if refreshErr == nil || errors.Is(refreshErr, backup.ErrRepositoryMissing) || errors.Is(refreshErr, backup.ErrRepositoryBusy) {
				return nil
			}
			slog.WarnContext(refreshCtx, "Failed to refresh backup repository stats", "repositoryId", entry.repository.ID, "error", refreshErr)
			mu.Lock()
			failures = append(failures, fmt.Errorf("%s: %w", entry.repository.ID, refreshErr))
			mu.Unlock()
			return nil
		})
	}
	_ = group.Wait()
	if len(failures) > 0 {
		return types.BackupStorageStats{}, errors.Join(failures...)
	}
	return s.Get(ctx)
}

func (s *Service) refresh(ctx context.Context, dockerClient *mobyClient.Client, entry target) error {
	heldCtx, release, err := s.deps.Engine.TryHoldRepository(ctx, entry.repository.ID)
	if err != nil {
		return err
	}
	defer release()
	password := entry.password
	if entry.volume {
		password, err = s.deps.Repository.RepositoryPassword(heldCtx, dockerClient, entry.repository, backup.PasswordOptions{ReadOnly: true})
		if err != nil {
			return err
		}
	}
	return s.deps.Engine.RefreshStats(heldCtx, dockerClient, entry.repository, password)
}

func (s *Service) targets(ctx context.Context, dockerClient *mobyClient.Client) ([]target, error) {
	var targets []target
	systemKey, keyErr := s.deps.RecoveryKeys.Get(ctx, backup.RecoveryKeyConfigID)
	if keyErr != nil && !errors.Is(keyErr, backup.ErrRecoveryKeyNotConfigured) {
		return nil, keyErr
	}
	if keyErr == nil {
		systemRepositories, err := s.deps.SystemRepositories(ctx, dockerClient)
		if err != nil {
			return nil, err
		}
		for _, repository := range systemRepositories {
			targets = append(targets, target{repository: repository, password: systemKey, volume: false})
		}
	}
	volumeRepositories, err := s.deps.Repository.Repositories(ctx, dockerClient, backup.LocalRepositoryOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	for _, repository := range volumeRepositories {
		targets = append(targets, target{repository: repository, password: "", volume: true})
	}
	return targets, nil
}

func mergeEnvironments(environments []types.EnvironmentData[types.BackupStorageStats]) types.BackupStorageStats {
	merged := types.BackupStorageStats{LastBackup: nil, Repositories: []types.BackupRepositoryStats{}}
	for _, environment := range environments {
		merged.Repositories = append(merged.Repositories, environment.Data.Repositories...)
		if latest := environment.Data.LastBackup; latest != nil && (merged.LastBackup == nil || latest.CreatedAt.After(merged.LastBackup.CreatedAt)) {
			merged.LastBackup = latest
		}
	}
	return merged
}
