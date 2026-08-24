// Package backup owns the shared Rustic backup engine used by volume and
// system backups: typed repository operations, per-repository serialization
// through the actor runtime, and run admission.
package backup

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	rusticruntime "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/rustic"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// Admission scopes shared by the backup engine and the per-policy job
// registries so scheduled and manual runs contend on the same leases.
const (
	VolumeAdmissionScope = "volume-backup"
	SystemAdmissionScope = "system-backup"
)

// Repository addresses one Rustic repository. ID is the stable serialization
// key: operations against the same ID run serially, different IDs concurrently.
type Repository struct {
	ID          string
	Environment []string
	Mounts      []mount.Mount
}

// Snapshot is the result of a completed Rustic backup operation.
type Snapshot struct {
	ID   string `json:"id"`
	Size int64  `json:"-"`
}

type rusticSnapshotOutputInternal struct {
	ID      string `json:"id"`
	Summary struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
	} `json:"summary"`
}

// RestoreOptions selects what a snapshot restore writes and where.
type RestoreOptions struct {
	// DeleteExtra removes files in the target that are absent from the snapshot.
	DeleteExtra bool
	// SourcePath restores only this path inside the snapshot when non-empty.
	SourcePath string
	// DestinationPath overrides the restore target path inside the helper
	// container; the target mount's path is used when empty.
	DestinationPath string
}

// Engine executes typed Rustic operations through the official Rustic image.
// Concurrency is actor-owned: one executor per repository ID plus the shared
// application admission gate for run exclusivity.
type Engine struct {
	imageService *image.ImageService
	runtime      *actors.Runtime
	admission    *actors.Gate[actors.AdmissionKey]
	lifecycleCtx context.Context
	executors    *actors.StateMap[string, *actors.Executor]
}

// NewEngine creates the shared backup engine on the actor runtime. ctx is the
// application lifecycle context repository executors are spawned on.
func NewEngine(ctx context.Context, runtime *actors.Runtime, admission *actors.Gate[actors.AdmissionKey], imageService *image.ImageService) *Engine {
	return &Engine{
		imageService: imageService,
		runtime:      runtime,
		admission:    admission,
		lifecycleCtx: ctx,
		executors:    actors.NewStateMap[string, *actors.Executor](),
	}
}

// TryAcquireRun admits at most one in-flight backup run per (scope, id),
// shared between scheduled jobs and manual API triggers.
func (e *Engine) TryAcquireRun(ctx context.Context, scope, id string) (*actors.Lease[actors.AdmissionKey], bool, error) {
	if e == nil {
		return nil, false, errors.New("backup engine is unavailable")
	}
	return e.admission.TryAcquire(ctx, actors.AdmissionKey{Scope: scope, ID: id})
}

// Stop joins every repository executor.
func (e *Engine) Stop(ctx context.Context) error {
	if e == nil {
		return nil
	}
	executors, err := e.executors.Drain(ctx, "stop backup repository executors")
	if err != nil {
		return err
	}
	var stopErr error
	for _, executor := range executors {
		stopErr = errors.Combine(stopErr, executor.Stop(ctx))
	}
	return stopErr
}

// CreateSnapshot backs the source mount up into the repository and returns the
// created snapshot. The repository is initialized on first use.
func (e *Engine) CreateSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, label string, source mount.Mount) (Snapshot, error) {
	output, err := e.runInternal(ctx, dockerClient, repository, password,
		[]string{"backup", "--init", "--json", "--as-path", "/", "--host", "arcane", "--label", label, "--", source.Target},
		source,
	)
	if err != nil {
		return Snapshot{}, err
	}
	var decoded rusticSnapshotOutputInternal
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		return Snapshot{}, fmt.Errorf("failed to decode Rustic snapshot: %w", err)
	}
	if decoded.ID == "" {
		return Snapshot{}, errors.New("rustic did not return a snapshot ID")
	}
	return Snapshot{ID: decoded.ID, Size: decoded.Summary.TotalBytesProcessed}, nil
}

// RestoreSnapshot restores a snapshot (or one path inside it) onto the target mount.
func (e *Engine) RestoreSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID string, target mount.Mount, options RestoreOptions) error {
	command := []string{"restore"}
	if options.DeleteExtra {
		command = append(command, "--delete")
	}
	source := snapshotID + ":/"
	if options.SourcePath != "" {
		source = snapshotID + ":/" + strings.TrimPrefix(options.SourcePath, "/")
	}
	destination := options.DestinationPath
	if destination == "" {
		destination = target.Target
	}
	command = append(command, "--", source, destination)
	_, err := e.runInternal(ctx, dockerClient, repository, password, command, target)
	return err
}

// ListSnapshotFiles returns every path recorded in the snapshot.
func (e *Engine) ListSnapshotFiles(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID string) ([]string, error) {
	output, err := e.runInternal(ctx, dockerClient, repository, password, []string{"ls", "--json", "--recursive", "--", snapshotID + ":/"})
	if err != nil {
		return nil, fmt.Errorf("failed to list Rustic snapshot: %w", err)
	}
	var files []string
	if err := json.Unmarshal([]byte(output), &files); err != nil {
		return nil, fmt.Errorf("failed to decode Rustic file list: %w", err)
	}
	return files, nil
}

// ReadSnapshotTextFile returns one text file from a snapshot.
func (e *Engine) ReadSnapshotTextFile(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID, filePath string) (string, error) {
	output, err := e.runInternal(ctx, dockerClient, repository, password, []string{
		"dump", "--archive", "content", "--", snapshotID + ":/" + strings.TrimPrefix(filePath, "/"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to read Rustic snapshot file: %w", err)
	}
	return output, nil
}

// DiscoveredSnapshot describes one snapshot found in a repository.
type DiscoveredSnapshot struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Summary struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
	} `json:"summary"`
}

// ListSnapshots enumerates every snapshot in the repository.
func (e *Engine) ListSnapshots(ctx context.Context, dockerClient *client.Client, repository Repository, password string) ([]DiscoveredSnapshot, error) {
	output, err := e.runInternal(ctx, dockerClient, repository, password, []string{"snapshots", "--json"})
	if err != nil {
		return nil, err
	}
	var snapshots []DiscoveredSnapshot
	if err := json.Unmarshal([]byte(output), &snapshots); err != nil {
		return nil, fmt.Errorf("failed to decode Rustic snapshots: %w", err)
	}
	if len(snapshots) == 0 || snapshots[0].ID != "" {
		return snapshots, nil
	}
	// Newer Rustic versions group snapshots by (host, label).
	var groups []struct {
		Snapshots []DiscoveredSnapshot `json:"snapshots"`
	}
	if err := json.Unmarshal([]byte(output), &groups); err != nil {
		return nil, fmt.Errorf("failed to decode grouped Rustic snapshots: %w", err)
	}
	snapshots = snapshots[:0]
	for _, group := range groups {
		snapshots = append(snapshots, group.Snapshots...)
	}
	return snapshots, nil
}

// ForgetSnapshot removes the snapshot from the repository and prunes its data.
func (e *Engine) ForgetSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID string) error {
	_, err := e.runInternal(ctx, dockerClient, repository, password, []string{"forget", "--prune", "--", snapshotID})
	return err
}

// Replicate copies one snapshot between repositories by restoring it into a
// temporary volume and backing that volume up into the target repository. The
// source is read once from the repository, never from the live data. Rustic's
// native `copy` would move only missing packs, but it addresses the target via
// a TOML config profile, which the env-only Repository cannot express yet —
// the materialize-and-rebackup here trades disk and I/O for that simplicity.
func (e *Engine) Replicate(ctx context.Context, dockerClient *client.Client, from Repository, fromSnapshotID string, to Repository, password, label string) (Snapshot, error) {
	temporaryVolume := "arcane-rustic-copy-" + uuid.NewString()
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: temporaryVolume, Labels: volumehelper.Labels()}); err != nil {
		return Snapshot{}, fmt.Errorf("failed to create temporary Rustic copy volume: %w", err)
	}
	defer func() {
		_, _ = dockerClient.VolumeRemove(context.WithoutCancel(ctx), temporaryVolume, client.VolumeRemoveOptions{Force: true})
	}()
	copyMount := mount.Mount{Type: mount.TypeVolume, Source: temporaryVolume, Target: "/volume"}
	if err := e.RestoreSnapshot(ctx, dockerClient, from, password, fromSnapshotID, copyMount, RestoreOptions{DeleteExtra: true}); err != nil {
		return Snapshot{}, fmt.Errorf("failed to load Rustic snapshot for replication: %w", err)
	}
	copyMount.ReadOnly = true
	snapshot, err := e.CreateSnapshot(ctx, dockerClient, to, password, label, copyMount)
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to replicate Rustic snapshot: %w", err)
	}
	return snapshot, nil
}

func (e *Engine) executorForInternal(repositoryID string) (*actors.Executor, error) {
	if executor, ok := e.executors.Get(repositoryID); ok {
		return executor, nil
	}
	return actors.ApplyStateMap(e.lifecycleCtx, e.executors, "backup repository executor", func(values map[string]*actors.Executor) (*actors.Executor, bool, error) {
		if executor, ok := values[repositoryID]; ok {
			return executor, false, nil
		}
		executor, err := actors.NewExecutor(e.lifecycleCtx, e.runtime, "backup-repository", repositoryID, 3)
		if err != nil {
			return nil, false, err
		}
		values[repositoryID] = executor
		return executor, true, nil
	})
}

func (e *Engine) runInternal(ctx context.Context, dockerClient *client.Client, repository Repository, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	if e == nil {
		return "", errors.New("backup engine is unavailable")
	}
	if strings.TrimSpace(repository.ID) == "" {
		return "", errors.New("backup repository ID is required")
	}
	executor, err := e.executorForInternal(repository.ID)
	if err != nil {
		return "", err
	}
	return actors.Execute(ctx, executor, "rustic "+command[0], func(workCtx context.Context) (string, error) {
		return e.runContainerInternal(workCtx, dockerClient, repository, password, command, extraMounts...)
	}, nil)
}

func (e *Engine) ensureImageInternal(ctx context.Context, dockerClient *client.Client) error {
	if _, err := dockerClient.ImageInspect(ctx, rusticruntime.DefaultImage); err == nil {
		return nil
	}
	if e.imageService == nil {
		return errors.New("image service is unavailable")
	}
	if err := e.imageService.PullImage(ctx, rusticruntime.DefaultImage, io.Discard, common.SystemUser, nil); err != nil {
		return fmt.Errorf("failed to pull official Rustic image: %w", err)
	}
	return nil
}
func arcaneNetworkModeInternal(ctx context.Context, dockerClient *client.Client) container.NetworkMode {
	arcane, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil || arcane == nil || arcane.ID == "" {
		slog.DebugContext(ctx, "backup engine: running Rustic on the default network", "error", err)
		return ""
	}
	return container.NetworkMode("container:" + arcane.ID)
}

func (e *Engine) runContainerInternal(ctx context.Context, dockerClient *client.Client, repository Repository, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	if err := e.ensureImageInternal(ctx, dockerClient); err != nil {
		return "", err
	}
	mounts := append([]mount.Mount{}, repository.Mounts...)
	mounts = append(mounts, extraMounts...)
	return rusticruntime.Run(ctx, dockerClient, password, command, repository.Environment, mounts, arcaneNetworkModeInternal(ctx, dockerClient))
}
