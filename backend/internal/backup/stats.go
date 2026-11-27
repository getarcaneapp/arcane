package backup

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mobyClient "github.com/moby/moby/client"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/timeouts"
)

type (
	queuedStats struct {
		ctx        context.Context
		timer      *time.Timer
		client     *mobyClient.Client
		password   string
		repository Repository
	}

	rusticRepoInfo struct {
		Files struct {
			Repo []struct {
				Size int64 `json:"size"`
			} `json:"repo"`
		} `json:"files"`
		Index struct {
			Blobs []struct {
				DataSize int64 `json:"data_size"`
			} `json:"blobs"`
		} `json:"index"`
	}
)

const statsRefreshDelay = 10 * time.Second

var ErrRepositoryBusy = errors.New("the backup repository is busy")

func (e *Engine) Stats(ctx context.Context) ([]RepositoryStats, error) {
	var stats []RepositoryStats
	if err := e.db.WithContext(ctx).Order("id").Find(&stats).Error; err != nil {
		return nil, fmt.Errorf("failed to load backup repository stats: %w", err)
	}
	return stats, nil
}

func (e *Engine) RefreshStats(ctx context.Context, dockerClient *mobyClient.Client, repository Repository, password string) error {
	ctx, release, err := e.TryHoldRepository(ctx, repository.ID)
	if err != nil {
		return err
	}
	defer release()
	return e.saveStats(ctx, dockerClient, repository, password)
}

func (e *Engine) saveStats(ctx context.Context, dockerClient *mobyClient.Client, repository Repository, password string) error {
	info, err := e.runContainer(ctx, dockerClient, repository, password, []string{"repoinfo", "--json"})
	if err != nil && strings.Contains(err.Error(), rusticRepositoryMissingMessage) {
		if deleteErr := e.db.WithContext(ctx).Where("id = ?", repository.ID).Delete(&RepositoryStats{}).Error; deleteErr != nil {
			return fmt.Errorf("failed to clear backup repository stats: %w", deleteErr)
		}
		return ErrRepositoryMissing
	}
	if err != nil {
		return fmt.Errorf("failed to read backup repository info: %w", err)
	}
	var decoded rusticRepoInfo
	if decodeErr := json.Unmarshal([]byte(info), &decoded); decodeErr != nil {
		return fmt.Errorf("failed to decode backup repository info: %w", decodeErr)
	}
	listing, err := e.runContainer(ctx, dockerClient, repository, password, []string{"snapshots", "--json"})
	if err != nil {
		return fmt.Errorf("failed to list backup repository snapshots: %w", err)
	}
	snapshots, err := decodeSnapshots(listing)
	if err != nil {
		return err
	}
	var stored, raw, logical int64
	for _, file := range decoded.Files.Repo {
		stored += file.Size
	}
	for _, blob := range decoded.Index.Blobs {
		raw += blob.DataSize
	}
	for _, snapshot := range snapshots {
		logical += snapshot.Summary.TotalBytesProcessed
	}
	now := time.Now().UTC()
	if saveErr := e.db.WithContext(ctx).Save(&RepositoryStats{ //exhaustruct:enforce
		BaseModel:     database.BaseModel{ID: repository.ID, CreatedAt: now, UpdatedAt: &now},
		SnapshotCount: int64(len(snapshots)),
		StoredBytes:   stored,
		RawBytes:      raw,
		LogicalBytes:  logical,
	}).Error; saveErr != nil {
		return fmt.Errorf("failed to save backup repository stats: %w", saveErr)
	}
	return nil
}

func (e *Engine) queueStats(ctx context.Context, dockerClient *mobyClient.Client, repository Repository, password string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil {
		return
	}
	timer := func() *time.Timer {
		if queued := e.queuedStats[repository.ID]; queued != nil {
			queued.timer.Reset(statsRefreshDelay)
			return queued.timer
		}
		return time.AfterFunc(statsRefreshDelay, func() { e.refreshQueuedStats(repository.ID) })
	}()
	e.queuedStats[repository.ID] = &queuedStats{
		ctx:        context.WithValue(context.WithoutCancel(ctx), heldRepository{engine: e, id: repository.ID}, nil),
		timer:      timer,
		client:     dockerClient,
		repository: repository,
		password:   password,
	}
}

func (e *Engine) refreshQueuedStats(id string) {
	e.mu.Lock()
	queued := e.queuedStats[id]
	delete(e.queuedStats, id)
	e.mu.Unlock()
	if queued == nil {
		return
	}
	ctx, cancel := context.WithTimeout(queued.ctx, timeouts.DefaultJobRun)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	err := e.RefreshStats(ctx, queued.client, queued.repository, queued.password)
	switch {
	case errors.Is(err, ErrRepositoryBusy):
		e.queueStats(queued.ctx, queued.client, queued.repository, queued.password)
	case err != nil && !errors.Is(err, ErrRepositoryMissing):
		slog.WarnContext(queued.ctx, "Failed to refresh backup repository stats", "repositoryId", id, "error", err)
	}
}

func (e *Engine) repositoryLock(id string) chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	lock := e.repositories[id]
	if lock == nil {
		lock = make(chan struct{}, 1)
		e.repositories[id] = lock
	}
	return lock
}
