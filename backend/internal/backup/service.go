package backup

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/image"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/scheduler/schedule"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/rustic"
	s3util "getarcane.dev/arcane/v3/pkg/s3"
	"getarcane.dev/arcane/v3/pkg/selfcontainer"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Repository struct {
		ID          string
		Environment []string
		Mounts      []mount.Mount
	}

	Snapshot struct {
		ID   string `json:"id"`
		Size int64  `json:"-"`
	}

	rusticSnapshotOutput struct {
		ID      string `json:"id"`
		Summary struct {
			TotalBytesProcessed int64 `json:"total_bytes_processed"`
		} `json:"summary"`
	}

	RestoreOptions struct {
		// SourcePath restores only this path inside the snapshot when non-empty.
		SourcePath string
		// DestinationPath overrides the restore target path inside the helper
		// container; the target mount's path is used when empty.
		DestinationPath string
		// ExtraMounts are attached alongside the target, e.g. mounts nested inside it.
		ExtraMounts []mount.Mount
		// Globs filter what is restored; --delete still removes destination paths they exclude.
		Globs       []string
		DeleteExtra bool
	}

	SourceMountOptions struct {
		ContainerPath string
		Target        string
		Mounts        []container.MountPoint
		InContainer   bool
		// Writable mounts the source read-write, for a restore into it.
		Writable bool
	}

	// LocalRepositoryOptions picks how the local repository's storage is mounted.
	LocalRepositoryOptions struct {
		ReadOnly bool
	}

	// DestinationOptions describes which backup destinations hold snapshots.
	DestinationOptions struct {
		Local  bool
		Remote bool
	}

	CreateSnapshotInput struct {
		Mounts  []mount.Mount
		Sources []string
		AsPath  string
		Tags    []string
		Globs   []string
	}

	Engine struct {
		imageService *image.ImageService
		admission    *runs.Admission
		repositories map[string]*sync.Mutex
		// held parks admission a manual start won until the run that won it takes it over.
		held      map[types.SchedulerAdmissionKey]heldLease
		authorize func(context.Context, types.BackupRequester) error
		mu        sync.Mutex
	}

	heldLease struct {
		lease *runs.Lease
		run   string
	}

	DiscoveredSnapshot struct {
		ID      string    `json:"id"`
		Time    time.Time `json:"time"`
		Label   string    `json:"label"`
		Tags    []string  `json:"tags"`
		Summary struct {
			TotalBytesProcessed int64 `json:"total_bytes_processed"`
		} `json:"summary"`
	}

	DiscoveredRecords[T any] struct {
		ID       func(entry T) string
		Existing func(ctx context.Context, ids []string) ([]T, error)
		Create   func(ctx context.Context, entry T) error
		Entries  []T
	}

	RecoveryKeyStore struct {
		db *database.DB
	}

	PolicyReconciliation[P, U any] struct {
		DB       *database.DB
		ID       func(*P) string
		UpdateID func(U) string
		// New returns the blank policy for created entries, pre-scoped by the
		// caller (e.g. with the volume name set).
		New   func() P
		Build func(ctx context.Context, policy *P, update U) error
		// Persist overrides the default GORM transaction for settings-backed policies.
		Persist    func(ctx context.Context, policies []P) error
		Unregister func(ctx context.Context, policyID string)
		Reschedule func(ctx context.Context, policy *P)
		Domain     string
		Existing   []P
	}

	DisableRemoteRequest struct {
		Model         any
		ID            string
		DestinationID string
		Field         string
		LocalEnabled  bool
	}

	// ScheduledRemoteRequest names the S3 repository a scheduled run checks and the tables recording its snapshots.
	ScheduledRemoteRequest struct {
		DestinationID string
		Root          string
		Tables        []string
	}

	// VolumeRepository is the volumes data repository that volume and project backups share.
	VolumeRepository struct {
		deps           VolumeRepositoryDependencies
		legacyPassword string
		rekeyed        sync.Map
	}

	VolumeRepositoryDependencies struct {
		Docker           *dockerInternal.DockerClientService
		Engine           *Engine
		Settings         *settings.SettingsService
		S3Destinations   *s3.S3DestinationService
		RecoveryKeys     *RecoveryKeyStore
		BackupVolumeName string
		EncryptionKey    string
	}

	// Containers stops the containers a backup must not run under and starts them again.
	Containers struct {
		Docker *dockerInternal.DockerClientService
		Stop   func(ctx context.Context, containerID string, user types.Actor) error
		Start  func(ctx context.Context, containerID string, user types.Actor) error
	}

	// ContainerScope selects the containers mounting any of the volumes.
	ContainerScope struct {
		Volumes []string
		// RefuseArcaneWriters fails instead when an Arcane container mounts one of the volumes.
		RefuseArcaneWriters bool
	}
)

const (
	VolumeAdmissionScope           = "volume-backup"
	ProjectSnapshotTag             = "arcane-kind:project"
	SystemAdmissionScope           = "system-backup"
	SystemAdmissionID              = "system"
	RecoveryKeyConfigID            = "system-recovery"
	RunKeyPrefix                   = "run:"
	VolumeRoot                     = "arcane-volume-backups"
	EnvironmentSystemRoot          = "arcane-environment-recovery"
	rusticRepositoryMissingMessage = "No repository config file found"
	DeletionBatchSize              = 500
	dataDirectory                  = "data"

	RemoteDisabledMessage = "S3 backup storage is missing. Remote backups were disabled; edit the policy to resume."

	admissionWait = 6 * time.Hour

	volumeRepositoryPath     = "/repository/volumes"
	localVolumeRepositoryID  = "volumes:local"
	legacyVolumePasswordSalt = "arcane-volume-backups:"
	legacyEncryptionKey      = "arcane-dev-key-32-characters!!!"

	containerRecoveryTimeout  = 30 * time.Second
	containerRecoveryInterval = 500 * time.Millisecond
)

var (
	ErrRecoveryKeyNotConfigured = errors.New("recovery key is not configured")
	ErrBackupSettled            = errors.New("the checkpointed backup already failed")
	RecoveryKeyFormat           = regexp.MustCompile(`^[A-Z2-7]{6}(-[A-Z2-7]{6}){7}$`)

	ErrRemoteRepositoryMissing = errors.New("S3 backup storage is missing")
	ErrRepositoryMissing       = errors.New("the backup repository does not exist")

	// VolumeRepositoryTables record the snapshots that volume and project backups write under VolumeRoot.
	VolumeRepositoryTables = []string{"volume_backups", "project_backups"}
)

func RootSnapshotInput(source mount.Mount, tags ...string) CreateSnapshotInput {
	return CreateSnapshotInput{Mounts: []mount.Mount{source}, Sources: []string{source.Target}, AsPath: "/", Tags: tags}
}

func snapshotCommand(label string, input CreateSnapshotInput) ([]string, error) {
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

func NewRequester(ctx context.Context, requestedBy types.Actor, environmentID, permission string) types.BackupRequester {
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	return types.BackupRequester{UserID: requestedBy.ID, APIKeyID: keyID, EnvironmentID: environmentID, Permission: permission}
}

func NewEngine(admission *runs.Admission, imageService *image.ImageService, authorize func(context.Context, types.BackupRequester) error) *Engine {
	return &Engine{
		imageService: imageService,
		admission:    admission,
		authorize:    authorize,
		repositories: make(map[string]*sync.Mutex),
		held:         make(map[types.SchedulerAdmissionKey]heldLease),
	}
}

func (e *Engine) TryAcquireRun(ctx context.Context, scope, id string) (*runs.Lease, bool, error) {
	return e.admission.TryAcquire(ctx, types.SchedulerAdmissionKey{Scope: scope, ID: id})
}

// AwaitRun waits for the admission so overlapping backups run in turn instead of skipping.
func (e *Engine) AwaitRun(ctx context.Context, scope, id string) (*runs.Lease, error) {
	key := types.SchedulerAdmissionKey{Scope: scope, ID: id}
	lease, admitted, err := e.admission.TryAcquire(ctx, key)
	if err != nil || admitted {
		return lease, err
	}
	slog.InfoContext(ctx, "Backup waiting for another backup to finish", "scope", scope)
	waitCtx, cancel := context.WithTimeout(ctx, admissionWait)
	defer cancel()
	lease, err = e.admission.Acquire(waitCtx, key)
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		return nil, common.Classify(common.ErrTimeout, fmt.Errorf("timed out after %s waiting for another backup to finish", admissionWait))
	}
	return lease, err
}

func (e *Engine) Hold(scope, id, run string, lease *runs.Lease) func(context.Context) {
	key := types.SchedulerAdmissionKey{Scope: scope, ID: id}
	e.mu.Lock()
	e.held[key] = heldLease{run: run, lease: lease}
	e.mu.Unlock()
	return func(ctx context.Context) {
		e.mu.Lock()
		untaken := e.held[key].lease == lease
		if untaken {
			delete(e.held, key)
		}
		e.mu.Unlock()
		if untaken {
			lease.Release(ctx)
		}
	}
}

func (e *Engine) AcquireRun(ctx context.Context, scope, id, run string) (*runs.Lease, bool, error) {
	key := types.SchedulerAdmissionKey{Scope: scope, ID: id}
	e.mu.Lock()
	held, ok := e.held[key]
	if ok = ok && run != "" && held.run == run; ok {
		delete(e.held, key)
	}
	e.mu.Unlock()
	if ok {
		return held.lease, true, nil
	}
	return e.TryAcquireRun(ctx, scope, id)
}

func (e *Engine) Authorize(ctx context.Context, requester types.BackupRequester) error {
	return e.authorize(ctx, requester)
}

func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	held := e.held
	e.held = make(map[types.SchedulerAdmissionKey]heldLease)
	e.mu.Unlock()
	for _, held := range held {
		held.lease.Release(ctx)
	}
	return nil
}

func (e *Engine) CreateSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, label string, input CreateSnapshotInput) (Snapshot, error) {
	command, err := snapshotCommand(label, input)
	if err != nil {
		return Snapshot{}, err
	}
	output, err := e.run(ctx, dockerClient, repository, password, command, input.Mounts...)
	if err != nil {
		return Snapshot{}, err
	}
	var decoded rusticSnapshotOutput
	if unmarshalErr := json.Unmarshal([]byte(output), &decoded); unmarshalErr != nil {
		return Snapshot{}, fmt.Errorf("failed to decode Rustic snapshot: %w", unmarshalErr)
	}
	if decoded.ID == "" {
		return Snapshot{}, errors.New("rustic did not return a snapshot ID")
	}
	return Snapshot{ID: decoded.ID, Size: decoded.Summary.TotalBytesProcessed}, nil
}

func (e *Engine) RestoreSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID string, target mount.Mount, options RestoreOptions) error {
	command := []string{"restore", "--verify-existing"}
	if options.DeleteExtra {
		command = append(command, "--delete")
	}
	for _, glob := range options.Globs {
		command = append(command, "--glob", glob)
	}
	source := snapshotID + ":/" + strings.TrimPrefix(options.SourcePath, "/")
	command = append(command, "--", source, cmp.Or(options.DestinationPath, target.Target))
	mounts := append([]mount.Mount{target}, options.ExtraMounts...)
	_, err := e.run(ctx, dockerClient, repository, password, command, mounts...)
	return err
}

func (e *Engine) ListSnapshotFiles(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID, filePath string, recursive bool) ([]string, error) {
	command := []string{"ls", "--json"}
	if recursive {
		command = append(command, "--recursive")
	}
	filePath = strings.TrimSpace(filePath)
	cleanedPath := strings.Trim(filePath, "/")
	source := snapshotID + ":/" + cleanedPath
	if cleanedPath != "" && strings.HasSuffix(filePath, "/") {
		source += "/"
	}
	command = append(command, "--", source)
	output, err := e.run(ctx, dockerClient, repository, password, command)
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
		longOutput, runErr := e.run(ctx, dockerClient, repository, password, longCommand)
		if runErr != nil {
			return nil, fmt.Errorf("failed to list Rustic snapshot metadata: %w", runErr)
		}
		files, runErr = markSnapshotDirectories(files, longOutput)
		if runErr != nil {
			return nil, runErr
		}
	}
	return qualifySnapshotListing(files, cleanedPath), nil
}

func qualifySnapshotListing(files []string, prefix string) []string {
	qualified := slices.Clone(files)
	if prefix == "" {
		return qualified
	}
	for index, file := range qualified {
		qualified[index] = path.Join(prefix, strings.TrimPrefix(strings.TrimPrefix(file, "./"), "/")) + kit.Ternary(strings.HasSuffix(file, "/"), "/", "")
	}
	return qualified
}

func markSnapshotDirectories(files []string, longOutput string) ([]string, error) {
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

func (e *Engine) ReadSnapshotTextFile(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID, filePath string) (string, error) {
	output, err := e.run(ctx, dockerClient, repository, password, []string{
		"dump", "--archive", "content", "--", snapshotID + ":/" + strings.TrimPrefix(filePath, "/"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to read Rustic snapshot file: %w", err)
	}
	return output, nil
}

func RunSnapshotTag(runID string) string { return "arcane-run:" + runID }

func (s DiscoveredSnapshot) LocalRunID() string {
	for _, tag := range s.Tags {
		if runID, ok := strings.CutPrefix(tag, RunSnapshotTag("")); ok && runID != "" {
			return runID
		}
	}
	return "local-" + s.ID
}

func RecordDiscovered[T any](ctx context.Context, records DiscoveredRecords[T]) (int, error) {
	if len(records.Entries) == 0 {
		return 0, nil
	}
	ids := make([]string, len(records.Entries))
	for index, entry := range records.Entries {
		ids[index] = records.ID(entry)
	}
	existing, err := records.Existing(ctx, ids)
	if err != nil {
		return 0, err
	}
	recorded := make(map[string]bool, len(existing))
	for _, entry := range existing {
		recorded[records.ID(entry)] = true
	}
	created := 0
	for _, entry := range records.Entries {
		if recorded[records.ID(entry)] {
			continue
		}
		if createErr := records.Create(ctx, entry); createErr != nil {
			return created, fmt.Errorf("failed to save discovered backup: %w", createErr)
		}
		recorded[records.ID(entry)] = true
		created++
	}
	return created, nil
}

func (e *Engine) ConfirmRunSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, runID, snapshotID string) (Snapshot, error) {
	if !fullSnapshotID(snapshotID) {
		return Snapshot{}, errors.New("a full snapshot ID is required")
	}
	command := []string{"tag", "--add", RunSnapshotTag(runID), "--", snapshotID}
	if _, err := e.run(ctx, dockerClient, repository, password, command); err != nil {
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

func (e *Engine) FindRunSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, runID, snapshotID string) (Snapshot, bool, error) {
	snapshots, err := e.ListSnapshots(ctx, dockerClient, repository, password)
	if err != nil {
		return Snapshot{}, false, err
	}
	var found Snapshot
	for _, snapshot := range snapshots {
		if !kit.Ternary(snapshotID == "", slices.Contains(snapshot.Tags, RunSnapshotTag(runID)), snapshot.ID == snapshotID) {
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

func (e *Engine) ListSnapshots(ctx context.Context, dockerClient *client.Client, repository Repository, password string) ([]DiscoveredSnapshot, error) {
	output, err := e.run(ctx, dockerClient, repository, password, []string{"snapshots", "--json"})
	if err != nil && strings.Contains(err.Error(), rusticRepositoryMissingMessage) {
		return nil, fmt.Errorf("%w: %w", ErrRepositoryMissing, err)
	}
	if err != nil {
		return nil, err
	}
	return decodeSnapshots(output)
}

func decodeSnapshots(output string) ([]DiscoveredSnapshot, error) {
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
		members := []jsontext.Value{entry}
		if hasSnapshots {
			if groupEntries.Kind() != '[' {
				return nil, fmt.Errorf("invalid Rustic snapshot group %d: expected a snapshots array", index)
			}
			members = nil
			if err := json.Unmarshal(groupEntries, &members); err != nil {
				return nil, fmt.Errorf("failed to decode Rustic snapshot group %d: %w", index, err)
			}
		}
		for memberIndex, member := range members {
			location := kit.Ternary(hasSnapshots, fmt.Sprintf("group %d entry %d", index, memberIndex), fmt.Sprintf("listing entry %d", index))
			var memberFields map[string]jsontext.Value
			if member.Kind() != '{' {
				return nil, fmt.Errorf("invalid Rustic snapshot %s: expected a snapshot object", location)
			}
			if err := json.Unmarshal(member, &memberFields); err != nil {
				return nil, fmt.Errorf("invalid Rustic snapshot %s: failed to decode Rustic snapshot fields: %w", location, err)
			}
			if _, nested := memberFields["snapshots"]; nested {
				return nil, fmt.Errorf("invalid Rustic snapshot %s: expected a flat snapshot object", location)
			}
			var snapshot DiscoveredSnapshot
			if err := json.Unmarshal(member, &snapshot); err != nil {
				return nil, fmt.Errorf("invalid Rustic snapshot %s: failed to decode Rustic snapshot payload: %w", location, err)
			}
			if !fullSnapshotID(snapshot.ID) {
				return nil, fmt.Errorf("invalid Rustic snapshot %s: invalid full snapshot ID", location)
			}
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots, nil
}

func fullSnapshotID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (e *Engine) ForgetSnapshots(ctx context.Context, dockerClient *client.Client, repository Repository, password string, snapshotIDs []string) error {
	requested := make([]string, 0, len(snapshotIDs))
	for _, id := range snapshotIDs {
		if !fullSnapshotID(id) {
			return errors.New("a full snapshot ID is required")
		}
		requested = append(requested, strings.ToLower(id))
	}
	requested = kit.Unique(requested)
	unlock, err := e.lockRepository(ctx, repository.ID)
	if err != nil {
		return err
	}
	defer unlock()
	output, err := e.runContainer(ctx, dockerClient, repository, password, []string{"snapshots", "--json"})
	if err != nil && strings.Contains(err.Error(), rusticRepositoryMissingMessage) {
		return nil
	}
	if err != nil {
		return err
	}
	snapshots, err := decodeSnapshots(output)
	if err != nil {
		return err
	}
	existing := make(map[string]string, len(snapshots))
	for _, snapshot := range snapshots {
		existing[strings.ToLower(snapshot.ID)] = snapshot.ID
	}
	var listed []string
	for _, id := range requested {
		if listedID, found := existing[id]; found {
			listed = append(listed, listedID)
		}
	}
	if len(listed) == 0 && len(requested) > 0 {
		slog.DebugContext(ctx, "No requested snapshots found in repository; skipping forget", "repositoryId", repository.ID, "requested", requested)
		return nil
	}
	if len(listed) == 0 {
		_, err = e.runContainer(ctx, dockerClient, repository, password, []string{"prune"})
		return err
	}
	batches := slices.Collect(slices.Chunk(listed, DeletionBatchSize))
	for i, batch := range batches {
		command := slices.Concat([]string{"forget"}, kit.Ternary(i == len(batches)-1, []string{"--prune"}, nil), []string{"--"}, batch)
		if _, err = e.runContainer(ctx, dockerClient, repository, password, command); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) ChangeRepositoryPassword(ctx context.Context, dockerClient *client.Client, repository Repository, currentPassword, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new repository password is required")
	}
	_, err := e.run(ctx, dockerClient, repository, currentPassword, []string{"key", "password", "--new-password", newPassword})
	return err
}

func (e *Engine) RekeyRepository(ctx context.Context, dockerClient *client.Client, repository Repository, newPassword string, currentPasswords ...string) (string, error) {
	_, err := e.ListSnapshots(ctx, dockerClient, repository, newPassword)
	if err == nil || errors.Is(err, ErrRepositoryMissing) {
		return "", nil
	}
	for _, candidate := range currentPasswords {
		if candidate == "" || candidate == newPassword {
			continue
		}
		if changeErr := e.ChangeRepositoryPassword(ctx, dockerClient, repository, candidate, newPassword); changeErr == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("backup repository %s does not open with the recovery key: %w", repository.ID, err)
}

func (e *Engine) RekeyRepositories(ctx context.Context, dockerClient *client.Client, repositories []Repository, newPassword string, currentPasswords ...string) error {
	type rekeyed struct {
		previous   string
		repository Repository
	}
	changed := make([]rekeyed, 0, len(repositories))
	for _, repository := range repositories {
		previous, err := e.RekeyRepository(ctx, dockerClient, repository, newPassword, currentPasswords...)
		if err != nil {
			errs := []error{err}
			var failedRollbackErrs []error
			for _, candidate := range currentPasswords {
				if candidate == "" || candidate == newPassword {
					continue
				}
				_, rollbackErr := e.RekeyRepository(context.WithoutCancel(ctx), dockerClient, repository, candidate, newPassword)
				if rollbackErr == nil {
					failedRollbackErrs = nil
					break
				}
				failedRollbackErrs = append(failedRollbackErrs, fmt.Errorf("failed to restore the previous key on backup repository %s: %w", repository.ID, rollbackErr))
			}
			errs = append(errs, failedRollbackErrs...)
			for _, done := range changed {
				if _, rollbackErr := e.RekeyRepository(context.WithoutCancel(ctx), dockerClient, done.repository, done.previous, newPassword); rollbackErr != nil {
					errs = append(errs, fmt.Errorf("failed to restore the previous key on backup repository %s: %w", done.repository.ID, rollbackErr))
				}
			}
			return errors.Join(errs...)
		}
		if previous != "" {
			changed = append(changed, rekeyed{
				repository: repository,
				previous:   previous,
			})
		}
	}
	return nil
}

func (e *Engine) Replicate(ctx context.Context, dockerClient *client.Client, from Repository, fromSnapshotID string, to Repository, password, label string, tags ...string) (Snapshot, error) {
	temporaryVolume := "arcane-rustic-copy-" + uuid.New().String()
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: temporaryVolume, Labels: volumes.Labels()}); err != nil {
		return Snapshot{}, fmt.Errorf("failed to create temporary Rustic copy volume: %w", err)
	}
	defer func() {
		_, _ = dockerClient.VolumeRemove(context.WithoutCancel(ctx), temporaryVolume, client.VolumeRemoveOptions{Force: true})
	}()
	if err := e.RestoreSnapshot(ctx, dockerClient, from, password, fromSnapshotID, mount.Mount{
		Type:   mount.TypeVolume,
		Source: temporaryVolume,
		Target: "/volume",
	}, RestoreOptions{DeleteExtra: true}); err != nil {
		return Snapshot{}, fmt.Errorf("failed to load Rustic snapshot for replication: %w", err)
	}
	snapshot, err := e.CreateSnapshot(ctx, dockerClient, to, password, label, RootSnapshotInput(mount.Mount{
		Type:     mount.TypeVolume,
		Source:   temporaryVolume,
		Target:   "/volume",
		ReadOnly: true,
	}, tags...))
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to replicate Rustic snapshot: %w", err)
	}
	return snapshot, nil
}

func (e *Engine) run(ctx context.Context, dockerClient *client.Client, repository Repository, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	unlock, err := e.lockRepository(ctx, repository.ID)
	if err != nil {
		return "", err
	}
	defer unlock()
	return e.runContainer(ctx, dockerClient, repository, password, command, extraMounts...)
}

func (e *Engine) runContainer(ctx context.Context, dockerClient *client.Client, repository Repository, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	if _, err := dockerClient.ImageInspect(ctx, rustic.DefaultImage); err != nil {
		if pullErr := e.imageService.PullImage(ctx, rustic.DefaultImage, io.Discard, types.SystemUser, nil); pullErr != nil {
			return "", fmt.Errorf("failed to pull official Rustic image: %w", pullErr)
		}
	}
	arcane, inspectErr := selfcontainer.InspectCurrentArcaneContainer(ctx, dockerClient)
	if inspectErr != nil || kit.FromPtr(arcane).ID == "" {
		slog.DebugContext(ctx, "backup engine: running Rustic on the default network", "error", inspectErr)
	}
	trust, err := rustic.TrustMounts(ctx, dockerClient, dataDirectory, kit.FromPtr(arcane).Mounts)
	if err != nil {
		return "", err
	}
	return rustic.Run(
		ctx,
		dockerClient,
		password,
		command,
		repository.Environment,
		slices.Concat(repository.Mounts, extraMounts, trust),
		container.NetworkMode(kit.Ternary(kit.FromPtr(arcane).ID == "", "", "container:"+kit.FromPtr(arcane).ID)),
	)
}

func ValidateRecoveryKey(key string) error {
	if !RecoveryKeyFormat.MatchString(strings.TrimSpace(key)) {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: "recoveryKey", Err: errors.New("enter the generated recovery key (8 groups of 6 characters)")})
	}
	return nil
}

func GenerateRecoveryKey() (string, error) {
	raw := make([]byte, 30)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate recovery key: %w", err)
	}
	return formatRecoveryKey(raw), nil
}

func EnvironmentRecoveryKey(recoveryKey, environmentID string) string {
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(recoveryKey)))
	mac.Write([]byte("arcane-environment-recovery:" + environmentID))
	return formatRecoveryKey(mac.Sum(nil)[:30])
}

func formatRecoveryKey(raw []byte) string {
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	groups := make([]string, 0, len(encoded)/6)
	for i := 0; i < len(encoded); i += 6 {
		groups = append(groups, encoded[i:i+6])
	}
	return strings.Join(groups, "-")
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

func (s *RecoveryKeyStore) Delete(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&SystemBackupRecoveryConfig{}).Error; err != nil {
		return fmt.Errorf("failed to delete recovery key: %w", err)
	}
	return nil
}

func (s *RecoveryKeyStore) Get(ctx context.Context, id string) (string, error) {
	var config SystemBackupRecoveryConfig
	if err := s.db.WithContext(ctx).Where("id = ? AND encrypted_recovery_key <> ''", id).First(&config).Error; err != nil {
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

func (s *RecoveryKeyStore) Set(ctx context.Context, id, recoveryKey string) error {
	if err := ValidateRecoveryKey(recoveryKey); err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(recoveryKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt recovery key: %w", err)
	}
	if saveErr := s.db.WithContext(ctx).Save(&SystemBackupRecoveryConfig{BaseModel: database.BaseModel{ID: id}, EncryptedRecoveryKey: encrypted}).Error; saveErr != nil {
		return fmt.Errorf("failed to save recovery key: %w", saveErr)
	}
	return nil
}

func AcceptedTarget(backupID string) types.TargetOutcome {
	data, _ := json.Marshal(struct {
		BackupID string `json:"backupId"`
	}{BackupID: backupID})
	return types.TargetOutcome{
		ResourceType: "backup",
		ID:           "accepted:" + backupID,
		Status:       types.RunStatusQueued,
		RecoveryData: data,
	}
}

func AcceptedBackupID(run types.SchedulerRun) string {
	index := slices.IndexFunc(run.Outcome.Targets, func(target types.TargetOutcome) bool {
		return strings.HasPrefix(target.ID, "accepted:")
	})
	if index < 0 {
		return ""
	}
	return strings.TrimPrefix(run.Outcome.Targets[index].ID, "accepted:")
}

func (e *Engine) lockRepository(ctx context.Context, id string) (func(), error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("backup repository ID is required")
	}
	e.mu.Lock()
	lock := e.repositories[id]
	if lock == nil {
		lock = &sync.Mutex{}
		e.repositories[id] = lock
	}
	e.mu.Unlock()
	lock.Lock()
	if err := ctx.Err(); err != nil {
		lock.Unlock()
		return nil, err
	}
	return lock.Unlock, nil
}

func (r PolicyReconciliation[P, U]) Run(ctx context.Context, updates []U) error {
	byID := make(map[string]P, len(r.Existing))
	for i := range r.Existing {
		byID[r.ID(&r.Existing[i])] = r.Existing[i]
	}
	policies := make([]P, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		policy := r.New()
		if updateID := r.UpdateID(update); updateID != "" {
			var ok bool
			policy, ok = byID[updateID]
			if !ok {
				return fmt.Errorf("%s backup policy not found", r.Domain)
			}
			if _, duplicate := kept[updateID]; duplicate {
				return fmt.Errorf("duplicate %s backup policy", r.Domain)
			}
		}
		if err := r.Build(ctx, &policy, update); err != nil {
			return err
		}
		if policyID := r.ID(&policy); policyID != "" {
			kept[policyID] = struct{}{}
		}
		policies = append(policies, policy)
	}
	removed := slices.DeleteFunc(slices.Clone(r.Existing), func(policy P) bool {
		_, ok := kept[r.ID(&policy)]
		return ok
	})

	var persistErr error
	switch {
	case r.Persist != nil:
		persistErr = r.Persist(ctx, policies)
	default:
		persistErr = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for i := range policies {
				if saveErr := tx.Save(&policies[i]).Error; saveErr != nil {
					return saveErr
				}
			}
			if len(removed) == 0 {
				return nil
			}
			return tx.Delete(&removed).Error
		})
	}
	if persistErr != nil {
		return fmt.Errorf("failed to save %s backup policies: %w", r.Domain, persistErr)
	}
	for i := range removed {
		r.Unregister(ctx, r.ID(&removed[i]))
	}
	for i := range policies {
		r.Reschedule(ctx, &policies[i])
	}
	return nil
}

func ValidatePolicyUpdate(ctx context.Context, domain string, update types.UpdateBackupPolicy, s3Service *s3.S3DestinationService) (types.UpdateBackupPolicy, error) {
	normalized, err := schedule.Normalize(update.Schedule, domain+" backup")
	if err != nil {
		return update, common.Classify(common.ErrValidation, &types.FieldError{Field: "schedule", Err: err})
	}
	if update.RetentionCount < 0 || update.RetentionCount > 3650 {
		return update, errors.New("retentionCount must be between 0 and 3650")
	}
	if !update.LocalEnabled && !update.S3Enabled {
		return update, fmt.Errorf("select at least one %s backup destination", domain)
	}
	if update.S3Enabled && strings.TrimSpace(update.S3DestinationID) == "" {
		return update, fmt.Errorf("select an S3 destination for %s backups", domain)
	}
	if update.S3Enabled {
		if _, configurationErr := s3Service.Configuration(ctx, update.S3DestinationID); configurationErr != nil {
			return update, fmt.Errorf("select a valid S3 destination for %s backups", domain)
		}
	}
	return types.UpdateBackupPolicy{
		ID:              update.ID,
		Enabled:         update.Enabled,
		Schedule:        normalized,
		RetentionCount:  update.RetentionCount,
		StopContainers:  update.StopContainers,
		LocalEnabled:    update.LocalEnabled,
		S3Enabled:       update.S3Enabled,
		S3DestinationID: kit.Ternary(update.S3Enabled, update.S3DestinationID, ""),
	}, nil
}

func RemoteSnapshotChecker(ctx context.Context, destinations *s3.S3DestinationService) func(destinationID, root, snapshotID string) *bool {
	checked := make(map[string]*types.BackupRepositoryObservation)
	deadline := time.Now().Add(10 * time.Second)
	return func(destinationID, root, snapshotID string) *bool {
		if root == "" || destinationID == "" || snapshotID == "" {
			return nil
		}
		key := destinationID + ":" + root + ":" + snapshotID
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

func CheckRemoteRepository(ctx context.Context, destinations *s3.S3DestinationService, destinationID, root string) (types.BackupRepositoryObservation, error) {
	configuration, err := destinations.Configuration(ctx, destinationID)
	if err != nil {
		return types.BackupRepositoryObservation{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s3util.CheckRepository(checkCtx, configuration, root, "")
}

func DisableMissingRemote(remoteErr error, localEnabled bool, s3Enabled, enabled *bool, save func(field string) (bool, error)) (bool, error) {
	if !errors.Is(remoteErr, ErrRemoteRepositoryMissing) {
		return false, nil
	}
	saved, err := save(kit.Ternary(localEnabled, "s3_enabled", "enabled"))
	if err != nil || !saved {
		return false, err
	}
	*kit.Ternary(localEnabled, s3Enabled, enabled) = false
	return true, nil
}

func DisableStoredRemote(ctx context.Context, db *database.DB, req DisableRemoteRequest) (bool, error) {
	result := db.WithContext(ctx).Model(req.Model).
		Where("id = ? AND s3_destination_id = ? AND enabled = ? AND s3_enabled = ? AND local_enabled = ?", req.ID, req.DestinationID, true, true, req.LocalEnabled).
		Update(req.Field, false)
	return result.Error == nil && result.RowsAffected > 0, result.Error
}

// CheckScheduledRemote treats a missing repository as a first backup only when none of the tables retains a snapshot in it.
func CheckScheduledRemote(ctx context.Context, db *database.DB, destinations *s3.S3DestinationService, req ScheduledRemoteRequest) error {
	result, err := CheckRemoteRepository(ctx, destinations, req.DestinationID, req.Root)
	if err != nil || result.Available {
		return err
	}
	if result.Reason == s3util.RepositoryReasonMissingRepository {
		var retained int64
		for _, table := range req.Tables {
			var count int64
			if countRetainedSnapshotsErr := db.WithContext(ctx).Table(table).
				Where("s3_destination_id = ? AND COALESCE(remote_snapshot_id, '') <> ''", req.DestinationID).
				Count(&count).Error; countRetainedSnapshotsErr != nil {
				return countRetainedSnapshotsErr
			}
			retained += count
		}
		if retained == 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrRemoteRepositoryMissing, result.Reason)
}

func ExpiredRunIDs(ctx context.Context, db *database.DB, table, policyID string, keep int) ([]string, error) {
	var expired []string
	for _, status := range []string{"succeeded", "failed"} {
		var ids []string
		if err := db.WithContext(ctx).
			Table(table).
			Where(
				"policy_id = ? AND status = ? AND (COALESCE(local_snapshot_id, '') <> '' OR COALESCE(remote_snapshot_id, '') <> '')",
				policyID,
				status,
			).
			Order("created_at DESC").
			Offset(keep).
			Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		expired = append(expired, ids...)
	}
	return expired, nil
}

// ExpiredUnmanagedRunIDs updates orphan marks and returns a random batch of expired runs, so runs that keep failing to
// delete cannot hold back the rest.
func ExpiredUnmanagedRunIDs(ctx context.Context, db *database.DB, table string, orphanedIDs []string, sweep types.UnmanagedBackupSweep) ([]string, error) {
	cutoff := sweep.Now.Add(-sweep.Retention)
	var expired []string
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stamped []string
		if err := tx.Table(table).Where("orphaned_at IS NOT NULL").Pluck("id", &stamped).Error; err != nil {
			return fmt.Errorf("load backup orphan marks: %w", err)
		}
		orphaned := make(map[string]struct{}, len(orphanedIDs))
		for _, id := range orphanedIDs {
			orphaned[id] = struct{}{}
		}
		var managed []string
		for _, id := range stamped {
			if _, ok := orphaned[id]; ok {
				delete(orphaned, id)
				continue
			}
			managed = append(managed, id)
		}
		for batch := range slices.Chunk(managed, DeletionBatchSize) {
			if err := tx.Table(table).Where("id IN ?", batch).Update("orphaned_at", nil).Error; err != nil {
				return fmt.Errorf("clear backup orphan marks: %w", err)
			}
		}
		for batch := range slices.Chunk(slices.Collect(maps.Keys(orphaned)), DeletionBatchSize) {
			if err := tx.Table(table).Where("id IN ?", batch).Update("orphaned_at", sweep.Now).Error; err != nil {
				return fmt.Errorf("mark orphaned backups: %w", err)
			}
		}
		query := tx.Table(table).
			Where("status <> ?", "running").
			Where(tx.Where("trigger IN ? AND created_at < ?", []string{types.SystemBackupTriggerSafety, types.SystemBackupTriggerManager}, cutoff).Or("orphaned_at < ?", cutoff))
		if len(sweep.Within) > 0 {
			query = query.Where("id IN ?", sweep.Within)
		}
		return query.Order("RANDOM()").Limit(DeletionBatchSize).Pluck("id", &expired).Error
	})
	return expired, err
}

// Destination names where a backup's snapshots are kept.
func Destination(opts DestinationOptions) types.VolumeBackupDestination {
	switch {
	case opts.Local && opts.Remote:
		return types.VolumeBackupDestinationLocalS3
	case opts.Remote:
		return types.VolumeBackupDestinationS3
	default:
		return types.VolumeBackupDestinationLocal
	}
}

func NewVolumeRepository(deps VolumeRepositoryDependencies) *VolumeRepository {
	return &VolumeRepository{deps: deps, legacyPassword: kit.SHA256Hex(legacyVolumePasswordSalt + cmp.Or(deps.EncryptionKey, legacyEncryptionKey))}
}

// StorageMount mounts backup storage at target: Arcane's own /backups mount when it has one, otherwise the backup volume.
func (r *VolumeRepository) StorageMount(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error) {
	inspect, err := selfcontainer.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil {
		slog.WarnContext(ctx, "backup: failed to inspect arcane container for backup mount resolution, falling back to named volume", "error", err.Error())
	} else if mirrored := storageMountFromMounts(ctx, inspect.Mounts, target, readOnly); mirrored != nil {
		return *mirrored, nil
	}
	if _, inspectErr := r.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeInspectResult, error) {
		return dockerClient.VolumeInspect(ctx, r.deps.BackupVolumeName, client.VolumeInspectOptions{})
	}); inspectErr != nil {
		if _, createErr := r.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeCreateResult, error) {
			return dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
				Name:   r.deps.BackupVolumeName,
				Labels: map[string]string{selfcontainer.InternalResourceLabel: "true"},
			})
		}); createErr != nil {
			return mount.Mount{}, fmt.Errorf("failed to create backup volume: %w", createErr)
		}
	}
	return mount.Mount{Type: mount.TypeVolume, Source: r.deps.BackupVolumeName, Target: target, ReadOnly: readOnly}, nil
}

// StorageVolume names the Docker volume StorageMount mounts: the volume at Arcane's own /backups, or the backup volume
// when /backups is not mounted. It is empty when /backups is a bind mount, which no Docker volume holds.
func (r *VolumeRepository) StorageVolume(ctx context.Context) string {
	inspect, err := r.deps.Docker.Call(ctx, selfcontainer.InspectCurrentArcaneContainer)
	if err == nil {
		if mirrored := docker.MountForDestination(inspect.Mounts, "/backups", "/backups"); mirrored != nil {
			if mirrored.Type != mount.TypeVolume {
				return ""
			}
			return mirrored.Source
		}
	}
	return r.deps.BackupVolumeName
}

func storageMountFromMounts(ctx context.Context, mounts []container.MountPoint, target string, readOnly bool) *mount.Mount {
	mirroredMount := docker.MountForDestination(mounts, "/backups", target)
	if mirroredMount == nil {
		return nil
	}
	if !readOnly && mirroredMount.ReadOnly {
		slog.WarnContext(ctx, "backup: requested writable backup mount but source is read-only; writes may fail")
	}
	mirroredMount.ReadOnly = readOnly
	return mirroredMount
}

func (r *VolumeRepository) Local(ctx context.Context, dockerClient *client.Client, opts LocalRepositoryOptions) (Repository, error) {
	storage, err := r.StorageMount(ctx, dockerClient, "/repository", opts.ReadOnly)
	if err != nil {
		return Repository{}, err
	}
	return Repository{
		ID:          localVolumeRepositoryID,
		Environment: []string{"RUSTIC_REPOSITORY=" + volumeRepositoryPath},
		Mounts:      []mount.Mount{storage},
	}, nil
}

// LocalInitialized reports false only when the mirrored /backups mount provably has no local repository yet.
func (r *VolumeRepository) LocalInitialized(ctx context.Context, dockerClient *client.Client) bool {
	inspect, err := selfcontainer.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil || docker.MountForDestination(inspect.Mounts, "/backups", "/backups") == nil {
		return true
	}
	_, err = acfs.Host{}.Stat(ctx, path.Join("/backups", path.Base(volumeRepositoryPath), "config"), acfstypes.StatOptions{FollowFinal: true})
	return !errors.Is(err, fs.ErrNotExist)
}

// Remote addresses one instance's root on the destination; empty instanceID means this instance.
func (r *VolumeRepository) Remote(ctx context.Context, destinationID, instanceID string) (Repository, error) {
	instanceID = cmp.Or(strings.TrimSpace(instanceID), strings.TrimSpace(r.deps.Settings.GetSettingsConfig().InstanceID.Value))
	if instanceID == "" {
		return Repository{}, errors.New("arcane instance ID is unavailable")
	}
	configuration, err := r.deps.S3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return Repository{}, fmt.Errorf("the selected S3 backup destination is not configured: %w", err)
	}
	return Repository{
		ID:          "volumes:s3:" + destinationID + ":" + instanceID,
		Environment: configuration.RusticEnvironment(VolumeRoot, instanceID),
	}, nil
}

// Password returns the recovery key, or the legacy password before one is configured.
func (r *VolumeRepository) Password(ctx context.Context, dockerClient *client.Client, repositories ...Repository) (string, error) {
	key, err := r.deps.RecoveryKeys.Get(ctx, RecoveryKeyConfigID)
	if errors.Is(err, ErrRecoveryKeyNotConfigured) {
		return r.legacyPassword, nil
	}
	if err != nil {
		return "", err
	}
	// Re-key each legacy repository once per process; one that opens with neither password is left for the caller to create or report.
	for _, repository := range repositories {
		if done, _ := r.rekeyed.Load(repository.ID); done == key {
			continue
		}
		if repository.ID == localVolumeRepositoryID {
			writable, localErr := r.Local(ctx, dockerClient, LocalRepositoryOptions{})
			if localErr != nil {
				slog.WarnContext(ctx, "could not open the local volume backup repository for re-keying", "error", localErr.Error())
				continue
			}
			repository = writable
		}
		previous, rekeyErr := r.deps.Engine.RekeyRepository(ctx, dockerClient, repository, key, r.legacyPassword)
		if rekeyErr != nil {
			slog.DebugContext(ctx, "volume backup repository was not re-keyed", "repository", repository.ID, "error", rekeyErr.Error())
			continue
		}
		if previous != "" {
			slog.InfoContext(ctx, "Re-keyed volume backup repository to the recovery key", "repository", repository.ID)
		}
		r.rekeyed.Store(repository.ID, key)
	}
	return key, nil
}

// Rekey re-keys this instance's own volumes repositories to the recovery key; other instances re-key their own roots.
func (r *VolumeRepository) Rekey(ctx context.Context, previousKey, newKey string) error {
	dockerClient, err := r.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	local, err := r.Local(ctx, dockerClient, LocalRepositoryOptions{})
	if err != nil {
		return err
	}
	repositories := []Repository{local}
	destinations, listErr := r.deps.S3Destinations.ListAllS3Destinations(ctx)
	if listErr != nil {
		return fmt.Errorf("failed to list S3 destinations for re-key: %w", listErr)
	}
	for _, destination := range destinations {
		remote, remoteErr := r.Remote(ctx, destination.ID, "")
		if remoteErr != nil {
			return remoteErr
		}
		repositories = append(repositories, remote)
	}
	if rekeyErr := r.deps.Engine.RekeyRepositories(ctx, dockerClient, repositories, newKey, previousKey, r.legacyPassword); rekeyErr != nil {
		return rekeyErr
	}
	for _, repository := range repositories {
		r.rekeyed.Store(repository.ID, newKey)
	}
	return nil
}

// StopContainers stops the running containers in scope, skipping Arcane's own and its helpers, and returns those it stopped.
func (c *Containers) StopContainers(ctx context.Context, scope ContainerScope, user types.Actor) ([]container.Summary, error) {
	containers, err := c.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
		return dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list running containers before backup: %w", err)
	}

	eligible := make([]container.Summary, 0, len(containers.Items))
	arcaneOwned := make([]container.Summary, 0, 2)
	for _, candidate := range containers.Items {
		if strings.EqualFold(candidate.Labels["com.getarcaneapp.arcane"], "true") || strings.EqualFold(candidate.Labels["com.getarcaneapp.arcane.agent"], "true") {
			arcaneOwned = append(arcaneOwned, candidate)
			continue
		}
		// Arcane helpers auto-remove on stop, which would leave nothing to restart.
		if strings.EqualFold(candidate.Labels[selfcontainer.InternalResourceLabel], "true") {
			continue
		}
		eligible = append(eligible, candidate)
	}
	var selected []string
	for _, volumeName := range scope.Volumes {
		// Restores rewrite the volume in place, so refuse rather than corrupt data under a labeled container.
		if writers := docker.FilterContainersUsingVolume(arcaneOwned, volumeName); scope.RefuseArcaneWriters && len(writers) > 0 {
			return nil, fmt.Errorf("volume %s is mounted by a running Arcane-managed container; restoring under it would corrupt its data", volumeName)
		}
		selected = append(selected, docker.FilterContainersUsingVolume(eligible, volumeName)...)
	}
	stopped := make([]container.Summary, 0, len(selected))
	for _, candidate := range eligible {
		if !slices.Contains(selected, candidate.ID) {
			continue
		}
		if stopErr := c.Stop(ctx, candidate.ID, user); stopErr != nil {
			stillStopped, restartErr := c.StartContainers(context.WithoutCancel(ctx), stopped, user)
			return stillStopped, errors.Join(fmt.Errorf("failed to stop container %s before backup: %w", candidate.ID, stopErr), restartErr)
		}
		stopped = append(stopped, candidate)
	}
	return stopped, nil
}

// StartContainers starts stopped containers again, or the Compose replicas that replaced them, and returns those it could not start.
//
//nolint:gocognit // recovery retries must reconcile IDs, names, and Compose identities in one bounded loop
func (c *Containers) StartContainers(ctx context.Context, stoppedContainers []container.Summary, user types.Actor) ([]container.Summary, error) {
	recoveryCtx, cancel := context.WithTimeout(ctx, containerRecoveryTimeout)
	defer cancel()

	remaining := append([]container.Summary(nil), stoppedContainers...)
	lastErrors := make(map[string]error, len(stoppedContainers))
	for len(remaining) > 0 {
		currentContainers, listErr := c.Docker.ListContainers(recoveryCtx, false)
		if listErr == nil {
			currentByID := make(map[string]container.Summary, len(currentContainers))
			currentByName := make(map[string]container.Summary, len(currentContainers))
			for _, current := range currentContainers {
				currentByID[current.ID] = current
				if name := docker.ContainerNameFromNames(current.Names); name != "" {
					currentByName[name] = current
				}
			}

			nextRemaining := make([]container.Summary, 0, len(remaining))
			for _, stopped := range remaining {
				current, found := currentByID[stopped.ID]
				if !found {
					name := docker.ContainerNameFromNames(stopped.Names)
					current, found = currentByName[name]
				}
				if !found {
					current, found = projects.FindComposeReplica(currentContainers, stopped.Labels)
				}
				if !found {
					nextRemaining = append(nextRemaining, stopped)
					continue
				}
				if current.State == container.StateRunning || current.State == container.StateRestarting {
					if current.ID != stopped.ID {
						slog.InfoContext(ctx, "backup: container was replaced during backup and is already running", "previousContainer", stopped.ID, "currentContainer", current.ID)
					}
					continue
				}
				if startErr := c.Start(recoveryCtx, current.ID, user); startErr != nil {
					lastErrors[stopped.ID] = startErr
					nextRemaining = append(nextRemaining, stopped)
					continue
				}
				if current.ID != stopped.ID {
					slog.InfoContext(ctx, "backup: restarted replacement container after backup", "previousContainer", stopped.ID, "currentContainer", current.ID)
				}
			}
			remaining = nextRemaining
		} else {
			for _, stopped := range remaining {
				lastErrors[stopped.ID] = listErr
			}
		}

		if len(remaining) == 0 {
			return nil, nil
		}
		timer := time.NewTimer(containerRecoveryInterval)
		select {
		case <-recoveryCtx.Done():
			timer.Stop()
			var restartErr error
			for _, stopped := range remaining {
				if lastErr := lastErrors[stopped.ID]; lastErr != nil {
					restartErr = errors.Join(restartErr, fmt.Errorf("failed to restart container %s after backup: %w", stopped.ID, lastErr))
				} else {
					restartErr = errors.Join(restartErr, fmt.Errorf("failed to restart container %s after backup: replacement did not appear within %s", stopped.ID, containerRecoveryTimeout))
				}
			}
			return remaining, restartErr
		case <-timer.C:
		}
	}
	return nil, nil
}

// CurrentMounts returns Arcane's own mounts when it runs in a container.
func CurrentMounts(ctx context.Context, dockerClient *client.Client) (mounts []container.MountPoint, inContainer bool, err error) {
	if !selfcontainer.RunningInContainer() {
		return nil, false, nil
	}
	inspect, err := selfcontainer.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil {
		return nil, true, fmt.Errorf("failed to inspect Arcane container mounts: %w", err)
	}
	return inspect.Mounts, true, nil
}

// SourceMounts mounts ContainerPath at Target through the host mounts behind it, including nested ones.
func SourceMounts(opts SourceMountOptions) ([]mount.Mount, error) {
	if !opts.InContainer {
		return []mount.Mount{{Type: mount.TypeBind, Source: opts.ContainerPath, Target: opts.Target, ReadOnly: !opts.Writable}}, nil
	}
	primary := docker.MountForSubpath(opts.Mounts, opts.ContainerPath, opts.Target)
	if primary == nil {
		return nil, fmt.Errorf("%s must be mounted into the Arcane container from a bind or named volume for backups", opts.ContainerPath)
	}
	nested := docker.NestedMounts(opts.Mounts, opts.ContainerPath, opts.Target)
	result := make([]mount.Mount, 0, 1+len(nested))
	for _, resolved := range append([]mount.Mount{*primary}, nested...) {
		result = append(result, mount.Mount{ //exhaustruct:enforce
			Type:           resolved.Type,
			Source:         resolved.Source,
			Target:         resolved.Target,
			ReadOnly:       !opts.Writable,
			Consistency:    resolved.Consistency,
			BindOptions:    resolved.BindOptions,
			VolumeOptions:  resolved.VolumeOptions,
			ImageOptions:   resolved.ImageOptions,
			TmpfsOptions:   resolved.TmpfsOptions,
			ClusterOptions: resolved.ClusterOptions,
		})
	}
	return result, nil
}
