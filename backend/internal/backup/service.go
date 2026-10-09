// Package backup owns the shared Rustic backup engine used by volume and
// system backups: typed repository operations, per-repository serialization
// and durable run admission.
package backup

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/rustic"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	s3util "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/s3"
)

const (
	VolumeAdmissionScope = "volume-backup"
	SystemAdmissionScope = "system-backup"
	RecoveryKeyConfigID  = "system-recovery"

	// rusticRepositoryMissingMessage is how Rustic reports an uninitialized repository.
	rusticRepositoryMissingMessage = "No repository config file found"
)

type Repository struct {
	ID          string
	Environment []string
	Mounts      []mount.Mount
}

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
	// ExtraMounts are attached alongside the target, e.g. mounts nested inside it.
	ExtraMounts []mount.Mount
}

// CreateSnapshotInput describes what one snapshot captures: the mounts to
// attach to the helper and the helper paths to back up. AsPath rewrites the
// snapshot path of a single source and cannot combine with several.
type CreateSnapshotInput struct {
	Mounts  []mount.Mount
	Sources []string
	AsPath  string
	Tags    []string
	Globs   []string
}

// RootSnapshotInput captures one mount with its contents at the snapshot root.
func RootSnapshotInput(source mount.Mount, tags ...string) CreateSnapshotInput {
	return CreateSnapshotInput{Mounts: []mount.Mount{source}, Sources: []string{source.Target}, AsPath: "/", Tags: tags}
}

func snapshotCommandInternal(label string, input CreateSnapshotInput) ([]string, error) {
	if len(input.Sources) == 0 {
		return nil, errors.New("at least one snapshot source is required")
	}
	if input.AsPath != "" && len(input.Sources) > 1 {
		return nil, errors.New("a snapshot path rewrite requires a single source")
	}
	command := []string{"backup", "--init", "--json", "--host", "arcane", "--label", label}
	for _, tag := range input.Tags {
		command = append(command, "--tag", tag)
	}
	for _, glob := range input.Globs {
		command = append(command, "--glob", glob)
	}
	if input.AsPath != "" {
		command = append(command, "--as-path", input.AsPath)
	}
	command = append(command, "--")
	return append(command, input.Sources...), nil
}

// Engine owns repository serialization and application-owned backup workers.
type Engine struct {
	imageService   *image.ImageService
	admission      *runs.Admission
	lifecycleCtx   context.Context
	cancel         context.CancelFunc
	workers        sync.WaitGroup
	stopping       bool
	mu             sync.Mutex
	repositories   map[string]*sync.Mutex
	service        *actor.Service
	handlers       map[string]func(context.Context, string, []byte, bool) error
	failures       map[string]func(context.Context, string, []byte, error) error
	leases         map[string]*runs.Lease
	unresolved     map[string]backup.DurableRunCommand
	executionReady func() bool
	authorize      func(context.Context, backup.DurableRunCommand) error
}

func NewEngine(ctx context.Context, admission *runs.Admission, imageService *image.ImageService) *Engine {
	runCtx, cancel := context.WithCancel(ctx)
	return &Engine{
		cancel:       cancel,
		imageService: imageService,
		admission:    admission,
		lifecycleCtx: runCtx,
		repositories: make(
			map[string]*sync.Mutex,
		),
		handlers: make(
			map[string]func(
				context.Context,
				string,
				[]byte,
				bool,
			) error,
		),
		failures: make(
			map[string]func(
				context.Context,
				string,
				[]byte,
				error,
			) error,
		),
		leases: make(
			map[string]*runs.Lease,
		),
		unresolved: make(map[string]backup.DurableRunCommand),
	}
}

func (e *Engine) TryAcquireRun(ctx context.Context, scope, id string) (*runs.Lease, bool, error) {
	if e == nil || e.admission == nil {
		return nil, false, errors.New("backup engine is unavailable")
	}
	return e.admission.TryAcquire(ctx, scheduler.AdmissionKey{Scope: scope, ID: id})
}

func (e *Engine) Stop(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	e.stopping = true
	e.cancel()
	e.mu.Unlock()
	done := make(chan struct{})
	go func() {
		e.workers.Wait()
		e.mu.Lock()
		leases := e.leases
		e.leases = make(map[string]*runs.Lease)
		e.mu.Unlock()
		for _, lease := range leases {
			lease.Release(ctx)
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CreateSnapshot backs the input's sources up into the repository as one
// snapshot. The repository is initialized on first use.
func (e *Engine) CreateSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, label string, input CreateSnapshotInput) (Snapshot, error) {
	command, err := snapshotCommandInternal(label, input)
	if err != nil {
		return Snapshot{}, err
	}
	output, err := e.runInternal(ctx, dockerClient, repository, password, command, input.Mounts...)
	if err != nil {
		return Snapshot{}, err
	}
	var decoded rusticSnapshotOutputInternal
	if unmarshalErr := json.Unmarshal([]byte(output), &decoded); unmarshalErr != nil {
		return Snapshot{}, fmt.Errorf("failed to decode Rustic snapshot: %w", unmarshalErr)
	}
	if decoded.ID == "" {
		return Snapshot{}, errors.New("rustic did not return a snapshot ID")
	}
	return Snapshot{ID: decoded.ID, Size: decoded.Summary.TotalBytesProcessed}, nil
}

// RestoreSnapshot restores a snapshot (or one path inside it) onto the target mount.
func (e *Engine) RestoreSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID string, target mount.Mount, options RestoreOptions) error {
	command := []string{"restore", "--verify-existing"}
	if options.DeleteExtra {
		command = append(command, "--delete")
	}
	source := snapshotID + ":/"
	if options.SourcePath != "" {
		source = snapshotID + ":/" + strings.TrimPrefix(options.SourcePath, "/")
	}
	destination := cmp.Or(options.DestinationPath, target.Target)
	command = append(command, "--", source, destination)
	mounts := append([]mount.Mount{target}, options.ExtraMounts...)
	_, err := e.runInternal(ctx, dockerClient, repository, password, command, mounts...)
	return err
}

// ListSnapshotFiles lists a snapshot path and returns paths relative to the
// snapshot root. A supplied path is nonrecursive unless recursive is true.
func (e *Engine) ListSnapshotFiles(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID, filePath string, recursive bool) ([]string, error) {
	command := []string{"ls", "--json"}
	if recursive {
		command = append(command, "--recursive")
	}
	cleanedPath := strings.Trim(strings.TrimSpace(filePath), "/")
	source := snapshotID + ":/" + cleanedPath
	if cleanedPath != "" && strings.HasSuffix(strings.TrimSpace(filePath), "/") {
		source += "/"
	}
	command = append(command, "--", source)
	output, err := e.runInternal(ctx, dockerClient, repository, password, command)
	if err != nil {
		return nil, fmt.Errorf("failed to list Rustic snapshot: %w", err)
	}
	var files []string
	if unmarshalErr := json.Unmarshal([]byte(output), &files); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to decode Rustic file list: %w", unmarshalErr)
	}
	if !recursive && len(files) > 0 {
		// Rustic's JSON listing omits node types; the stable long listing restores them.
		longCommand := slices.Clone(command)
		longCommand[1] = "--long"
		longOutput, runErr := e.runInternal(ctx, dockerClient, repository, password, longCommand)
		if runErr != nil {
			return nil, fmt.Errorf("failed to list Rustic snapshot metadata: %w", runErr)
		}
		files, runErr = markSnapshotDirectoriesInternal(files, longOutput)
		if runErr != nil {
			return nil, runErr
		}
	}
	return qualifySnapshotListingInternal(files, cleanedPath), nil
}

func qualifySnapshotListingInternal(files []string, snapshotPath string) []string {
	qualified := slices.Clone(files)
	prefix := strings.Trim(strings.TrimSpace(snapshotPath), "/")
	if prefix == "" {
		return qualified
	}
	for index, file := range qualified {
		directory := strings.HasSuffix(file, "/")
		relative := strings.TrimPrefix(strings.TrimPrefix(file, "./"), "/")
		qualified[index] = path.Join(prefix, relative)
		if directory {
			qualified[index] += "/"
		}
	}
	return qualified
}

func markSnapshotDirectoriesInternal(files []string, longOutput string) ([]string, error) {
	if len(files) == 0 {
		if strings.TrimSpace(longOutput) != "" {
			return nil, errors.New("rustic file and metadata listings have different lengths")
		}
		return []string{}, nil
	}
	lines := strings.Split(strings.ReplaceAll(longOutput, "\r\n", "\n"), "\n")
	if len(lines) != len(files) {
		return nil, errors.New("rustic file and metadata listings have different lengths")
	}
	marked := slices.Clone(files)
	for index, line := range lines {
		if line == "" {
			return nil, errors.New("rustic returned empty file metadata")
		}
		if line[0] == 'd' && !strings.HasSuffix(marked[index], "/") {
			marked[index] += "/"
		}
	}
	return marked, nil
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
	Label   string    `json:"label"`
	Tags    []string  `json:"tags"`
	Summary struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
	} `json:"summary"`
}

// RunSnapshotTag links committed snapshots to their durable backup record.
func RunSnapshotTag(runID string) string { return "arcane-run:" + runID }

// ConfirmRunSnapshot makes a validated snapshot discoverable by run ID.
func (e *Engine) ConfirmRunSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, runID, snapshotID string) (Snapshot, error) {
	if !fullSnapshotIDInternal(snapshotID) {
		return Snapshot{}, errors.New("a full snapshot ID is required")
	}
	command := []string{"tag", "--add", RunSnapshotTag(runID), "--", snapshotID}
	if _, err := e.runInternal(ctx, dockerClient, repository, password, command); err != nil {
		return Snapshot{}, err
	}
	// Tagging rewrites the snapshot and changes its ID.
	snapshot, found, err := e.FindRunSnapshot(ctx, dockerClient, repository, password, runID, "")
	if err != nil {
		return Snapshot{}, err
	}
	if !found {
		return Snapshot{}, errors.New("confirmed backup snapshot is unavailable")
	}
	return snapshot, nil
}

// FindRunSnapshot verifies a known snapshot or recovers its committed response by run tag.
func (e *Engine) FindRunSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, runID, snapshotID string) (Snapshot, bool, error) {
	snapshots, err := e.ListSnapshots(ctx, dockerClient, repository, password)
	if err != nil {
		return Snapshot{}, false, err
	}
	var found Snapshot
	for _, snapshot := range snapshots {
		match := snapshotID != "" && snapshot.ID == snapshotID
		if snapshotID == "" {
			match = slices.Contains(snapshot.Tags, RunSnapshotTag(runID))
		}
		if !match {
			continue
		}
		if found.ID != "" {
			return Snapshot{}, false, errors.New("multiple snapshots match the backup run")
		}
		found = Snapshot{ID: snapshot.ID, Size: snapshot.Summary.TotalBytesProcessed}
	}
	if snapshotID != "" && found.ID == "" {
		return Snapshot{}, false, errors.New("recorded backup snapshot is unavailable")
	}
	return found, found.ID != "", nil
}

// ListSnapshots enumerates every snapshot in the repository.
func (e *Engine) ListSnapshots(ctx context.Context, dockerClient *client.Client, repository Repository, password string) ([]DiscoveredSnapshot, error) {
	output, err := e.runInternal(ctx, dockerClient, repository, password, []string{"snapshots", "--json"})
	if err != nil {
		return nil, err
	}
	return decodeSnapshotsInternal(output)
}

func decodeSnapshotsInternal(output string) ([]DiscoveredSnapshot, error) {
	trimmedOutput := strings.TrimSpace(output)
	if trimmedOutput == "" || trimmedOutput[0] != '[' {
		return nil, errors.New("invalid Rustic snapshot listing: expected an array")
	}
	var entries []jsontext.Value
	if err := json.Unmarshal([]byte(output), &entries); err != nil {
		return nil, fmt.Errorf("failed to decode Rustic snapshots: %w", err)
	}

	snapshots := make([]DiscoveredSnapshot, 0, len(entries))
	grouped := false
	for index, entry := range entries {
		if entry.Kind() != '{' {
			return nil, fmt.Errorf("invalid Rustic snapshot listing entry %d: expected an object", index)
		}
		var fields map[string]jsontext.Value
		if err := json.Unmarshal(entry, &fields); err != nil {
			return nil, fmt.Errorf("failed to decode Rustic snapshot listing entry %d: %w", index, err)
		}
		_, hasID := fields["id"]
		groupEntries, hasSnapshots := fields["snapshots"]
		if hasID == hasSnapshots {
			return nil, fmt.Errorf("invalid Rustic snapshot listing entry %d: expected an ID or snapshot group", index)
		}
		if index > 0 && grouped != hasSnapshots {
			return nil, errors.New("invalid Rustic snapshot listing: mixed flat and grouped entries")
		}
		grouped = hasSnapshots
		if !hasSnapshots {
			snapshot, err := decodeSnapshotInternal(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid Rustic snapshot listing entry %d: %w", index, err)
			}
			snapshots = append(snapshots, snapshot)
			continue
		}
		if groupEntries.Kind() != '[' {
			return nil, fmt.Errorf("invalid Rustic snapshot group %d: expected a snapshots array", index)
		}
		var members []jsontext.Value
		if err := json.Unmarshal(groupEntries, &members); err != nil {
			return nil, fmt.Errorf("failed to decode Rustic snapshot group %d: %w", index, err)
		}
		for memberIndex, member := range members {
			snapshot, err := decodeSnapshotInternal(member)
			if err != nil {
				return nil, fmt.Errorf("invalid Rustic snapshot group %d entry %d: %w", index, memberIndex, err)
			}
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots, nil
}

func decodeSnapshotInternal(raw jsontext.Value) (DiscoveredSnapshot, error) {
	if raw.Kind() != '{' {
		return DiscoveredSnapshot{}, errors.New("expected a snapshot object")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return DiscoveredSnapshot{}, fmt.Errorf("failed to decode Rustic snapshot fields: %w", err)
	}
	if _, grouped := fields["snapshots"]; grouped {
		return DiscoveredSnapshot{}, errors.New("expected a flat snapshot object")
	}
	var snapshot DiscoveredSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return DiscoveredSnapshot{}, fmt.Errorf("failed to decode Rustic snapshot payload: %w", err)
	}
	if !fullSnapshotIDInternal(snapshot.ID) {
		return DiscoveredSnapshot{}, errors.New("invalid full snapshot ID")
	}
	return snapshot, nil
}

func fullSnapshotIDInternal(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// ForgetSnapshots removes the snapshots and prunes their data in a single pass;
// with no IDs it only prunes, skipping an uninitialized repository.
func (e *Engine) ForgetSnapshots(ctx context.Context, dockerClient *client.Client, repository Repository, password string, snapshotIDs []string) error {
	requested := make([]string, 0, len(snapshotIDs))
	for _, id := range snapshotIDs {
		if !fullSnapshotIDInternal(id) {
			return errors.New("a full snapshot ID is required")
		}
		requested = append(requested, strings.ToLower(id))
	}
	requested = kit.Unique(requested)
	if e == nil {
		return errors.New("backup engine is unavailable")
	}
	if strings.TrimSpace(repository.ID) == "" {
		return errors.New("backup repository ID is required")
	}
	defer e.lockRepositoryInternal(repository.ID)()
	if err := ctx.Err(); err != nil {
		return err
	}
	output, err := e.runContainerInternal(ctx, dockerClient, repository, password, []string{"snapshots", "--json"})
	if len(snapshotIDs) == 0 && err != nil && strings.Contains(err.Error(), rusticRepositoryMissingMessage) {
		return nil
	}
	if err != nil {
		return err
	}
	snapshots, err := decodeSnapshotsInternal(output)
	if err != nil {
		return err
	}
	existing := make(map[string]string, len(snapshots))
	for _, snapshot := range snapshots {
		existing[strings.ToLower(snapshot.ID)] = snapshot.ID
	}
	command := []string{"forget", "--prune"}
	for _, id := range requested {
		if listedID, found := existing[id]; found {
			if len(command) == 2 {
				command = append(command, "--")
			}
			command = append(command, listedID)
		}
	}
	if len(command) == 2 {
		command = []string{"prune"}
	}
	_, err = e.runContainerInternal(ctx, dockerClient, repository, password, command)
	return err
}

// ChangeRepositoryPassword re-keys the repository; the scratch Rustic image has no shell, so the new password travels as an argument.
func (e *Engine) ChangeRepositoryPassword(ctx context.Context, dockerClient *client.Client, repository Repository, currentPassword, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new repository password is required")
	}
	_, err := e.runInternal(ctx, dockerClient, repository, currentPassword, []string{"key", "password", "--new-password", newPassword})
	return err
}

// Replicate copies one snapshot between repositories by restoring it into a
// temporary volume and backing that volume up into the target repository. The
// source is read once from the repository, never from the live data. Rustic's
// native `copy` would move only missing packs, but it addresses the target via
// a TOML config profile, which the env-only Repository cannot express yet —
// the materialize-and-rebackup here trades disk and I/O for that simplicity.
func (e *Engine) Replicate(ctx context.Context, dockerClient *client.Client, from Repository, fromSnapshotID string, to Repository, password, label string, tags ...string) (Snapshot, error) {
	temporaryVolume := "arcane-rustic-copy-" + uuid.New().String()
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
	snapshot, err := e.CreateSnapshot(ctx, dockerClient, to, password, label, RootSnapshotInput(copyMount, tags...))
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to replicate Rustic snapshot: %w", err)
	}
	return snapshot, nil
}

func (e *Engine) runInternal(ctx context.Context, dockerClient *client.Client, repository Repository, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	if e == nil {
		return "", errors.New("backup engine is unavailable")
	}
	if strings.TrimSpace(repository.ID) == "" {
		return "", errors.New("backup repository ID is required")
	}
	defer e.lockRepositoryInternal(repository.ID)()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return e.runContainerInternal(ctx, dockerClient, repository, password, command, extraMounts...)
}

func (e *Engine) ensureImageInternal(ctx context.Context, dockerClient *client.Client) error {
	if _, err := dockerClient.ImageInspect(ctx, rustic.DefaultImage); err == nil {
		return nil
	}
	if e.imageService == nil {
		return errors.New("image service is unavailable")
	}
	if err := e.imageService.PullImage(ctx, rustic.DefaultImage, io.Discard, user.SystemUser, nil); err != nil {
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
	return rustic.Run(ctx, dockerClient, password, command, repository.Environment, mounts, arcaneNetworkModeInternal(ctx, dockerClient))
}

var (
	ErrRecoveryKeyNotConfigured = errors.New("recovery key is not configured")

	// RecoveryKeyFormat is 8 hyphenated groups of 6 base32 characters, used verbatim as the Rustic password.
	RecoveryKeyFormat = regexp.MustCompile(`^[A-Z2-7]{6}(-[A-Z2-7]{6}){7}$`)
)

func ValidateRecoveryKey(key string) error {
	if !RecoveryKeyFormat.MatchString(strings.TrimSpace(key)) {
		return errors.New("enter the generated recovery key (8 groups of 6 characters)")
	}
	return nil
}

func GenerateRecoveryKey() (string, error) {
	raw := make([]byte, 30)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate recovery key: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	groups := make([]string, 0, len(encoded)/6)
	for i := 0; i < len(encoded); i += 6 {
		groups = append(groups, encoded[i:i+6])
	}
	return strings.Join(groups, "-"), nil
}

// RecoveryKeyStore persists the recovery key shared by the system and volume backup domains.
type RecoveryKeyStore struct {
	db *database.DB
}

func NewRecoveryKeyStore(db *database.DB) *RecoveryKeyStore {
	return &RecoveryKeyStore{db: db}
}

func (s *RecoveryKeyStore) Configured(ctx context.Context) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&SystemBackupRecoveryConfig{}).
		Where("id = ? AND encrypted_recovery_key <> ''", RecoveryKeyConfigID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to load recovery key status: %w", err)
	}
	return count > 0, nil
}

// Get returns the decrypted recovery key or ErrRecoveryKeyNotConfigured.
func (s *RecoveryKeyStore) Get(ctx context.Context) (string, error) {
	var config SystemBackupRecoveryConfig
	if err := s.db.WithContext(ctx).
		Where("id = ? AND encrypted_recovery_key <> ''", RecoveryKeyConfigID).
		First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrRecoveryKeyNotConfigured
		}
		return "", fmt.Errorf("failed to load recovery key: %w", err)
	}
	key, err := crypto.Decrypt(config.EncryptedRecoveryKey)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt recovery key: %w", err)
	}
	return key, nil
}

func (s *RecoveryKeyStore) Set(ctx context.Context, recoveryKey string) error {
	if err := ValidateRecoveryKey(recoveryKey); err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(recoveryKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt recovery key: %w", err)
	}
	config := SystemBackupRecoveryConfig{ID: RecoveryKeyConfigID, EncryptedRecoveryKey: encrypted}
	if saveRecoveryKeyErr := s.db.WithContext(ctx).Save(&config).Error; saveRecoveryKeyErr != nil {
		return fmt.Errorf("failed to save recovery key: %w", saveRecoveryKeyErr)
	}
	return nil
}

func (e *Engine) lockRepositoryInternal(id string) func() {
	e.mu.Lock()
	lock := e.repositories[id]
	if lock == nil {
		lock = &sync.Mutex{}
		e.repositories[id] = lock
	}
	e.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// PolicyReconciliation reconciles a full set of policy updates against the
// existing policies of one backup domain, then swaps the scheduled jobs.
type PolicyReconciliation[P, U any] struct {
	// Domain names the backup flavor in validation errors ("volume", "system").
	Domain string
	DB     *database.DB
	// Existing is every policy currently persisted for the reconciled scope.
	Existing []P
	ID       func(*P) string
	UpdateID func(U) string
	// New returns the blank policy for created entries, pre-scoped by the
	// caller (e.g. with the volume name set).
	New func() P
	// Build validates an update and applies it to an existing or new policy.
	Build func(ctx context.Context, policy *P, update U) error
	// Persist overrides the default GORM transaction for settings-backed policies.
	Persist    func(ctx context.Context, policies []P) error
	Unregister func(ctx context.Context, policyID string)
	Reschedule func(ctx context.Context, policy *P)
}

func (r PolicyReconciliation[P, U]) Run(ctx context.Context, updates []U) error {
	policies, kept, err := r.buildInternal(ctx, updates)
	if err != nil {
		return err
	}
	if persistErr := r.persistInternal(ctx, policies, kept); persistErr != nil {
		return fmt.Errorf("failed to save %s backup policies: %w", r.Domain, persistErr)
	}
	for i := range r.Existing {
		if _, ok := kept[r.ID(&r.Existing[i])]; !ok {
			r.Unregister(ctx, r.ID(&r.Existing[i]))
		}
	}
	for i := range policies {
		r.Reschedule(ctx, &policies[i])
	}
	return nil
}

func (r PolicyReconciliation[P, U]) persistInternal(ctx context.Context, policies []P, kept map[string]struct{}) error {
	if r.Persist != nil {
		return r.Persist(ctx, policies)
	}
	if r.DB == nil {
		return errors.New("backup policy database is unavailable")
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range policies {
			if saveErr := tx.Save(&policies[i]).Error; saveErr != nil {
				return saveErr
			}
		}
		for i := range r.Existing {
			if _, ok := kept[r.ID(&r.Existing[i])]; !ok {
				if deleteErr := tx.Delete(&r.Existing[i]).Error; deleteErr != nil {
					return deleteErr
				}
			}
		}
		return nil
	})
}

// ValidatePolicyUpdate applies the shared cron, retention, and destination rules used by backup policies.
func ValidatePolicyUpdate(ctx context.Context, domain string, update backup.UpdateBackupPolicy, s3Service *s3.S3DestinationService) (backup.UpdateBackupPolicy, error) {
	normalized, err := schedule.NormalizeSixField(update.Schedule, domain+" backup")
	if err != nil {
		return update, err
	}
	update.Schedule = normalized
	if update.RetentionCount < 0 || update.RetentionCount > 3650 {
		return update, errors.New("retentionCount must be between 0 and 3650")
	}
	if !update.LocalEnabled && !update.S3Enabled {
		return update, fmt.Errorf("select at least one %s backup destination", domain)
	}
	if update.S3Enabled {
		if strings.TrimSpace(update.S3DestinationID) == "" {
			return update, fmt.Errorf("select an S3 destination for %s backups", domain)
		}
		if s3Service == nil {
			return update, errors.New("S3 backup destinations are unavailable")
		}
		if _, configurationErr := s3Service.Configuration(ctx, update.S3DestinationID); configurationErr != nil {
			return update, fmt.Errorf("select a valid S3 destination for %s backups", domain)
		}
	} else {
		update.S3DestinationID = ""
	}
	return update, nil
}

// buildInternal maps the updates onto existing or new policy rows and reports
// which existing IDs survive the reconciliation.
func (r PolicyReconciliation[P, U]) buildInternal(ctx context.Context, updates []U) ([]P, map[string]struct{}, error) {
	byID := make(map[string]P, len(r.Existing))
	for i := range r.Existing {
		byID[r.ID(&r.Existing[i])] = r.Existing[i]
	}
	policies := make([]P, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		policy := r.New()
		updateID := r.UpdateID(update)
		if updateID != "" {
			var ok bool
			policy, ok = byID[updateID]
			if !ok {
				return nil, nil, fmt.Errorf("%s backup policy not found", r.Domain)
			}
			if _, duplicate := kept[updateID]; duplicate {
				return nil, nil, fmt.Errorf("duplicate %s backup policy", r.Domain)
			}
		}
		if err := r.Build(ctx, &policy, update); err != nil {
			return nil, nil, err
		}
		if policyID := r.ID(&policy); policyID != "" {
			kept[policyID] = struct{}{}
		}
		policies = append(policies, policy)
	}
	return policies, kept, nil
}

var ErrRemoteRepositoryMissing = errors.New("S3 backup storage is missing")

const RemoteDisabledMessage = "S3 backup storage is missing. Remote backups were disabled; edit the policy to resume."

// RemoteSnapshotChecker memoizes targeted checks within one response budget.
// A nil result means the remote copy could not be verified.
func RemoteSnapshotChecker(ctx context.Context, destinations *s3.S3DestinationService, root string) func(string, string) *bool {
	checked := make(map[string]*backup.RepositoryObservation)
	deadline := time.Now().Add(10 * time.Second)
	return func(destinationID, snapshotID string) *bool {
		if destinations == nil || root == "" || destinationID == "" || snapshotID == "" {
			return nil
		}
		key := destinationID + ":" + snapshotID
		observation, seen := checked[key]
		if !seen {
			checked[key] = nil
			checkCtx, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			configuration, err := destinations.Configuration(checkCtx, destinationID)
			if err != nil {
				return nil
			}
			result, err := s3util.CheckRepository(checkCtx, configuration, root, snapshotID)
			if err != nil {
				return nil
			}
			observation = &result
			checked[key] = observation
		}
		if observation == nil {
			return nil
		}
		return new(observation.Available && observation.SnapshotAvailable)
	}
}

// CheckRemoteRepository probes the destination's repository config within a bounded budget.
// Transport and credential failures are returned as errors so callers never forget snapshots blindly.
func CheckRemoteRepository(ctx context.Context, destinations *s3.S3DestinationService, destinationID, root string) (backup.RepositoryObservation, error) {
	if destinations == nil {
		return backup.RepositoryObservation{}, errors.New("S3 destinations are unavailable")
	}
	configuration, err := destinations.Configuration(ctx, destinationID)
	if err != nil {
		return backup.RepositoryObservation{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s3util.CheckRepository(checkCtx, configuration, root, "")
}

// CheckScheduledRemote permits first-use initialization, but detects lost repositories.
func CheckScheduledRemote(ctx context.Context, db *database.DB, destinations *s3.S3DestinationService, table, destinationID, root string) error {
	result, err := CheckRemoteRepository(ctx, destinations, destinationID, root)
	if err != nil || result.Available {
		return err
	}
	if result.Reason == s3util.RepositoryReasonMissingRepository {
		var retained int64
		if countRetainedSnapshotsErr := db.WithContext(ctx).Table(table).
			Where("s3_destination_id = ? AND COALESCE(remote_snapshot_id, '') <> ''", destinationID).
			Count(&retained).Error; countRetainedSnapshotsErr != nil {
			return countRetainedSnapshotsErr
		}
		if retained == 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrRemoteRepositoryMissing, result.Reason)
}

const backupRunTypeInternal = "backup-run"

type backupRunActorInternal struct {
	id      string
	engine  *Engine
	service *actor.Service
}

// RegisterRunKind binds a domain handler before the actor host starts.
func (e *Engine) RegisterRunKind(kind string, execute func(context.Context, string, []byte, bool) error, failures ...func(context.Context, string, []byte, error) error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[kind] = execute
	if len(failures) > 0 {
		e.failures[kind] = failures[0]
	}
}

func (e *Engine) SetAuthorize(authorize func(context.Context, backup.DurableRunCommand) error) {
	e.authorize = authorize
}

func (e *Engine) SetExecutionReady(ready func() bool) { e.executionReady = ready }

func (e *Engine) Register(runtime *francis.Runtime) error {
	e.service = runtime.Service()
	return runtime.RegisterActor(backupRunTypeInternal, func(id string, service *actor.Service) actor.Actor {
		return &backupRunActorInternal{id: id, engine: e, service: service}
	}, local.WithCapacityGroup("jobs", 4), local.WithCompletedJobRetention(7*24*time.Hour))
}

// SubmitDurableRun persists the accepted command before dispatching its work.
// A nil error means the run was accepted or its outcome is still unresolved;
// an error means the intent was confirmed absent and admission is the caller's.
func (e *Engine) SubmitDurableRun(ctx context.Context, command backup.DurableRunCommand, lease *runs.Lease) error {
	if e == nil || e.service == nil {
		return errors.New("backup actor host is unavailable")
	}
	if e.authorize != nil {
		if err := e.authorize(ctx, command); err != nil {
			return err
		}
	}
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		return errors.New("backup engine is stopping")
	}
	e.leases[command.RunID] = lease
	e.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	_, err := e.service.Invoke(ctx, backupRunTypeInternal, command.RunID, "submit", command)
	if err == nil {
		return nil
	}
	var state backup.DurableRunState
	readErr := e.service.GetState(ctx, backupRunTypeInternal, command.RunID, &state)
	switch {
	case readErr == nil && sameDurableCommandInternal(state.Command, command):
		slog.WarnContext(ctx, "backup run accepted after submit error", "runId", command.RunID, "error", err)
		return nil
	case readErr == nil || errors.Is(readErr, actor.ErrStateNotFound):
		e.mu.Lock()
		delete(e.leases, command.RunID)
		e.mu.Unlock()
		return err
	default:
		// Keep admission parked until reconciliation can read the intent back.
		slog.WarnContext(ctx, "backup run outcome unresolved", "runId", command.RunID, "error", err, "readError", readErr)
		e.mu.Lock()
		e.unresolved[command.RunID] = command
		e.mu.Unlock()
		return nil
	}
}

// AcquireDurableRun keeps the original admission, or reacquires it after a restart.
func (e *Engine) AcquireDurableRun(ctx context.Context, runID, scope, resourceID string) (*runs.Lease, bool, error) {
	e.mu.Lock()
	lease := e.leases[runID]
	delete(e.leases, runID)
	e.mu.Unlock()
	if lease != nil {
		return lease, true, nil
	}
	return e.TryAcquireRun(ctx, scope, resourceID)
}

func (a *backupRunActorInternal) Invoke(ctx context.Context, _ string, data actor.Envelope) (any, error) {
	var command backup.DurableRunCommand
	if err := data.Decode(&command); err != nil {
		return nil, err
	}
	var state backup.DurableRunState
	err := a.service.GetState(ctx, backupRunTypeInternal, a.id, &state)
	switch {
	case errors.Is(err, actor.ErrStateNotFound):
		state = backup.DurableRunState{Command: command, Status: scheduler.Queued}
		if setStateErr := a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil); setStateErr != nil {
			return nil, setStateErr
		}
	case err != nil:
		return nil, err
	case !sameDurableCommandInternal(state.Command, command):
		return nil, errors.New("backup run already exists with a different command")
	}
	if state.Status == scheduler.Succeeded || state.Status == scheduler.Failed || state.Status == scheduler.NeedsAttention {
		return nil, nil
	}
	// The intent is persisted, so ReconcileDispatches repairs a failed dispatch.
	if _, _, dispatchErr := a.service.Dispatch(ctx, backupRunTypeInternal, a.id, "execute", nil, actor.WithIdempotencyKey(a.id)); dispatchErr != nil {
		slog.WarnContext(ctx, "dispatch accepted backup run", "runId", a.id, "error", dispatchErr)
	}
	return nil, nil
}

func sameDurableCommandInternal(a, b backup.DurableRunCommand) bool {
	return a.UserID == b.UserID &&
		a.EnvironmentID == b.EnvironmentID &&
		a.Permission == b.Permission &&
		a.RequestedWithKey == b.RequestedWithKey &&
		a.Kind == b.Kind &&
		a.RunID == b.RunID &&
		a.ActivityID == b.ActivityID &&
		bytes.Equal(a.Payload, b.Payload)
}

func (a *backupRunActorInternal) Job(ctx context.Context, _ string, _ actor.Envelope) error {
	if a.engine.executionReady != nil && !a.engine.executionReady() {
		return actor.ErrJobRejected
	}
	a.engine.mu.Lock()
	if a.engine.stopping {
		a.engine.mu.Unlock()
		return actor.ErrJobRejected
	}
	a.engine.workers.Add(1)
	a.engine.mu.Unlock()
	defer a.engine.workers.Done()
	jobCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.engine.lifecycleCtx, cancel) //nolint:contextcheck // Cancels the inherited job context when the engine stops.
	defer stop()
	defer cancel()
	ctx = jobCtx

	var state backup.DurableRunState
	if err := a.service.GetState(ctx, backupRunTypeInternal, a.id, &state); err != nil {
		return err
	}
	if state.Status == scheduler.Succeeded || state.Status == scheduler.Failed || state.Status == scheduler.NeedsAttention {
		return nil
	}
	a.engine.mu.Lock()
	pendingLease := a.engine.leases[a.id]
	a.engine.mu.Unlock()
	defer pendingLease.Release(ctx)
	interrupted := state.Started && len(state.Targets) > 0
	state.Started = true
	state.Status = scheduler.Running
	if err := a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil); err != nil {
		return err
	}
	var progressMu sync.Mutex
	ctx = jobcontext.WithExecution(ctx, scheduler.Run{ID: a.id, EnvironmentID: "0", Outcome: scheduler.Outcome{Targets: state.Targets}}, func(target scheduler.TargetOutcome) error {
		progressMu.Lock()
		defer progressMu.Unlock()
		updated := false
		for i := range state.Targets {
			if state.Targets[i].ID == target.ID {
				if len(target.RecoveryData) == 0 {
					target.RecoveryData = state.Targets[i].RecoveryData
				}
				state.Targets[i] = target
				updated = true
				break
			}
		}
		if !updated {
			state.Targets = append(state.Targets, target)
		}
		return a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil)
	})
	runErr := a.executeInternal(ctx, state.Command, interrupted)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	state.Status = scheduler.Succeeded
	if runErr != nil {
		state.Status = scheduler.NeedsAttention
		state.Error = runErr.Error()
	}
	if err := a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s: %w", runErr.Error(), actor.ErrJobPermanentFailure)
	}
	return nil
}

func (a *backupRunActorInternal) executeInternal(ctx context.Context, command backup.DurableRunCommand, interrupted bool) (err error) {
	defer utils.RecoverToError(&err, "durable backup")
	a.engine.mu.Lock()
	handler := a.engine.handlers[command.Kind]
	failure := a.engine.failures[command.Kind]
	a.engine.mu.Unlock()
	if handler == nil {
		return errors.New("backup run kind is unavailable")
	}
	if a.engine.authorize != nil {
		if authorizeErr := a.engine.authorize(ctx, command); authorizeErr != nil {
			a.engine.mu.Lock()
			lease := a.engine.leases[a.id]
			delete(a.engine.leases, a.id)
			a.engine.mu.Unlock()
			defer lease.Release(ctx)
			if failure != nil {
				authorizeErr = errors.Join(authorizeErr, failure(ctx, a.id, command.Payload, authorizeErr))
			}
			return authorizeErr
		}
	}
	return handler(ctx, a.id, command.Payload, interrupted)
}

func (e *Engine) activeRunsInternal(ctx context.Context) ([]backup.DurableRunState, error) {
	result := []backup.DurableRunState{}
	for cursor := ""; ; {
		page, err := e.service.ListStates(ctx, backupRunTypeInternal, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, item := range page.States {
			if item.Data == nil {
				continue
			}
			var state backup.DurableRunState
			if decodeErr := item.Data.Decode(&state); decodeErr != nil {
				return nil, decodeErr
			}
			if state.Status == scheduler.Queued || state.Status == scheduler.Running || state.Status == scheduler.NeedsAttention {
				result = append(result, state)
			}
		}
		cursor = page.AfterID()
		if cursor == "" {
			break
		}
	}
	return result, nil
}

func (e *Engine) ActiveRunIDs(ctx context.Context) ([]string, error) {
	states, err := e.activeRunsInternal(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(states))
	for _, state := range states {
		ids = append(ids, state.Command.RunID)
		for _, target := range state.Targets {
			var checkpoint struct {
				BackupID string `json:"backupId"`
			}
			if len(target.RecoveryData) > 0 && json.Unmarshal(target.RecoveryData, &checkpoint) == nil && checkpoint.BackupID != "" {
				ids = append(ids, checkpoint.BackupID)
			}
		}
	}
	return ids, nil
}

func (e *Engine) ActiveActivityIDs(ctx context.Context) ([]string, error) {
	states, err := e.activeRunsInternal(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(states))
	for _, state := range states {
		if state.Command.ActivityID != "" {
			ids = append(ids, state.Command.ActivityID)
		}
	}
	return ids, nil
}

// ReconcileDispatches resolves unresolved submissions and repairs accepted
// commands whose dispatch was interrupted.
func (e *Engine) ReconcileDispatches(ctx context.Context) error {
	failures := e.resolveUnresolvedRunsInternal(ctx)
	states, err := e.activeRunsInternal(ctx)
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	for _, state := range states {
		if state.Status == scheduler.NeedsAttention {
			continue
		}
		if repairErr := e.repairDispatchInternal(ctx, state.Command.RunID); repairErr != nil {
			failures = append(failures, fmt.Errorf("repair backup run %s: %w", state.Command.RunID, repairErr))
		}
	}
	return errors.Join(failures...)
}

func (e *Engine) repairDispatchInternal(ctx context.Context, runID string) error {
	jobs, err := e.service.ListJobs(ctx, backupRunTypeInternal, runID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !job.Status.IsTerminal() {
			return nil
		}
		if job.Status == actor.JobStatusDeadLettered {
			_, retryJobErr := e.service.RetryJob(ctx, job.JobID)
			return retryJobErr
		}
		if deleteJobErr := e.service.DeleteJob(ctx, backupRunTypeInternal, runID, job.JobID); deleteJobErr != nil && !errors.Is(deleteJobErr, actor.ErrJobNotFound) {
			return deleteJobErr
		}
	}
	_, _, err = e.service.Dispatch(ctx, backupRunTypeInternal, runID, "execute", nil, actor.WithIdempotencyKey(runID))
	return err
}

// resolveUnresolvedRunsInternal reads back submissions whose outcome was
// unknown. Found intents continue as accepted; confirmed absence releases
// admission and fails the run through its kind's failure handler.
func (e *Engine) resolveUnresolvedRunsInternal(ctx context.Context) []error {
	e.mu.Lock()
	pending := maps.Clone(e.unresolved)
	e.mu.Unlock()
	var failures []error
	for runID, command := range pending {
		var state backup.DurableRunState
		readErr := e.service.GetState(ctx, backupRunTypeInternal, runID, &state)
		if readErr != nil && !errors.Is(readErr, actor.ErrStateNotFound) {
			failures = append(failures, fmt.Errorf("read back backup run %s: %w", runID, readErr))
			continue
		}
		e.mu.Lock()
		lease := e.leases[runID]
		failure := e.failures[command.Kind]
		if readErr != nil {
			delete(e.leases, runID)
		}
		e.mu.Unlock()
		if readErr != nil {
			lease.Release(ctx)
			// Stay unresolved until the failure is recorded, so a later tick retries it.
			if failure != nil {
				if failErr := failure(ctx, runID, command.Payload, errors.New("backup run was not accepted")); failErr != nil {
					failures = append(failures, fmt.Errorf("fail backup run %s: %w", runID, failErr))
					continue
				}
			}
		}
		e.mu.Lock()
		delete(e.unresolved, runID)
		e.mu.Unlock()
	}
	return failures
}

// ExpiredRunIDs returns the IDs of succeeded runs with snapshots that fall
// outside the newest keep entries for one policy, oldest last. Callers delete
// each run through their own delete path so snapshots are forgotten too.
func ExpiredRunIDs(ctx context.Context, db *database.DB, table, policyID string, keep int) ([]string, error) {
	var ids []string
	err := db.WithContext(ctx).
		Table(table).
		Where(
			"policy_id = ? AND status = ? AND (COALESCE(local_snapshot_id, '') <> '' OR COALESCE(remote_snapshot_id, '') <> '')",
			policyID,
			"succeeded",
		).
		Order("created_at DESC").
		Offset(keep).
		Pluck("id", &ids).Error
	return ids, err
}
