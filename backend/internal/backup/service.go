// Package backup owns the shared Rustic backup engine used by volume and
// system backups: typed repository operations, per-repository serialization
// and run admission.
package backup

import (
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
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/rustic"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	s3util "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/s3"
)

const (
	VolumeAdmissionScope = "volume-backup"
	SystemAdmissionScope = "system-backup"
	// SystemAdmissionID is the one admission key system and system-managed volume backups share.
	SystemAdmissionID   = "system"
	RecoveryKeyConfigID = "system-recovery"
	// VolumeRoot is the S3 prefix under which each instance keeps its volume backup repository.
	VolumeRoot = "arcane-volume-backups"

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

type rusticSnapshotOutput struct {
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

// Engine owns repository serialization and backup admission.
type Engine struct {
	imageService *image.ImageService
	admission    *runs.Admission
	mu           sync.Mutex
	repositories map[string]*sync.Mutex
	// held parks admission a manual start won until the run that won it takes it over.
	held      map[scheduler.AdmissionKey]heldLease
	authorize func(context.Context, backup.Requester) error
}

// heldLease is a parked lease and the run entitled to take it.
type heldLease struct {
	run   string
	lease *runs.Lease
}

// NewRequester captures the user, and API key if any, behind a manual backup request.
func NewRequester(ctx context.Context, requestedBy user.Actor, environmentID, permission string) backup.Requester {
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	return backup.Requester{UserID: requestedBy.ID, APIKeyID: keyID, EnvironmentID: environmentID, Permission: permission}
}

// NewEngine builds the backup engine on the shared admission gate.
func NewEngine(admission *runs.Admission, imageService *image.ImageService) *Engine {
	return &Engine{
		imageService: imageService,
		admission:    admission,
		repositories: make(map[string]*sync.Mutex),
		held:         make(map[scheduler.AdmissionKey]heldLease),
	}
}

// TryAcquireRun takes the admission lease for one backup resource, reporting false while another run holds it.
func (e *Engine) TryAcquireRun(ctx context.Context, scope, id string) (*runs.Lease, bool, error) {
	if e == nil || e.admission == nil {
		return nil, false, errors.New("backup engine is unavailable")
	}
	return e.admission.TryAcquire(ctx, scheduler.AdmissionKey{Scope: scope, ID: id})
}

// Hold parks a lease won by a manual start for run until that run's task takes it over with AcquireRun.
// The returned func releases the lease if the run never took it, such as a start that failed to submit.
func (e *Engine) Hold(scope, id, run string, lease *runs.Lease) func(context.Context) {
	key := scheduler.AdmissionKey{Scope: scope, ID: id}
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

// AcquireRun takes over the lease held for run, or acquires admission otherwise, such as after a restart.
// A lease parked for another run stays parked, so that run's task still finds its admission.
func (e *Engine) AcquireRun(ctx context.Context, scope, id, run string) (*runs.Lease, bool, error) {
	key := scheduler.AdmissionKey{Scope: scope, ID: id}
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

// SetAuthorize sets the check Authorize applies to manual backup requesters.
func (e *Engine) SetAuthorize(authorize func(context.Context, backup.Requester) error) {
	e.authorize = authorize
}

// Authorize checks that the requester may still run the backup.
func (e *Engine) Authorize(ctx context.Context, requester backup.Requester) error {
	if e.authorize == nil {
		return nil
	}
	return e.authorize(ctx, requester)
}

// Stop releases leases that no workflow task took over.
func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	held := e.held
	e.held = make(map[scheduler.AdmissionKey]heldLease)
	e.mu.Unlock()
	for _, held := range held {
		held.lease.Release(ctx)
	}
	return nil
}

// CreateSnapshot backs the input's sources up into the repository as one
// snapshot. The repository is initialized on first use.
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

// RestoreSnapshot restores a snapshot (or one path inside it) onto the target mount.
func (e *Engine) RestoreSnapshot(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID string, target mount.Mount, options RestoreOptions) error {
	command := []string{"restore", "--verify-existing"}
	if options.DeleteExtra {
		command = append(command, "--delete")
	}
	source := snapshotID + ":/" + strings.TrimPrefix(options.SourcePath, "/")
	command = append(command, "--", source, cmp.Or(options.DestinationPath, target.Target))
	mounts := append([]mount.Mount{target}, options.ExtraMounts...)
	_, err := e.run(ctx, dockerClient, repository, password, command, mounts...)
	return err
}

// ListSnapshotFiles lists a snapshot path and returns paths relative to the
// snapshot root. A supplied path is nonrecursive unless recursive is true.
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

// qualifySnapshotListing prefixes listed files with the cleaned snapshot path they were listed under.
func qualifySnapshotListing(files []string, prefix string) []string {
	qualified := slices.Clone(files)
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

// ReadSnapshotTextFile returns one text file from a snapshot.
func (e *Engine) ReadSnapshotTextFile(ctx context.Context, dockerClient *client.Client, repository Repository, password, snapshotID, filePath string) (string, error) {
	output, err := e.run(ctx, dockerClient, repository, password, []string{
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
	output, err := e.run(ctx, dockerClient, repository, password, []string{"snapshots", "--json"})
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

// ForgetSnapshots removes the snapshots and prunes their data in a single pass;
// with no IDs it only prunes, skipping an uninitialized repository.
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
	if len(snapshotIDs) == 0 && err != nil && strings.Contains(err.Error(), rusticRepositoryMissingMessage) {
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
	_, err = e.runContainer(ctx, dockerClient, repository, password, command)
	return err
}

// ChangeRepositoryPassword re-keys the repository; the scratch Rustic image has no shell, so the new password travels as an argument.
func (e *Engine) ChangeRepositoryPassword(ctx context.Context, dockerClient *client.Client, repository Repository, currentPassword, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new repository password is required")
	}
	_, err := e.run(ctx, dockerClient, repository, currentPassword, []string{"key", "password", "--new-password", newPassword})
	return err
}

// Replicate copies one snapshot into another repository through a temporary volume, never the live data.
// Rustic's native `copy` needs a TOML profile for the target, which the env-only Repository cannot express.
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

func (e *Engine) run(ctx context.Context, dockerClient *client.Client, repository Repository, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	unlock, err := e.lockRepository(ctx, repository.ID)
	if err != nil {
		return "", err
	}
	defer unlock()
	return e.runContainer(ctx, dockerClient, repository, password, command, extraMounts...)
}

// runContainer runs one Rustic command; callers hold the repository lock.
func (e *Engine) runContainer(ctx context.Context, dockerClient *client.Client, repository Repository, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	if _, err := dockerClient.ImageInspect(ctx, rustic.DefaultImage); err != nil {
		if e.imageService == nil {
			return "", errors.New("image service is unavailable")
		}
		if pullErr := e.imageService.PullImage(ctx, rustic.DefaultImage, io.Discard, user.SystemUser, nil); pullErr != nil {
			return "", fmt.Errorf("failed to pull official Rustic image: %w", pullErr)
		}
	}
	var networkMode container.NetworkMode
	arcane, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil || arcane == nil || arcane.ID == "" {
		slog.DebugContext(ctx, "backup engine: running Rustic on the default network", "error", err)
	} else {
		networkMode = container.NetworkMode("container:" + arcane.ID)
	}
	mounts := append(slices.Clone(repository.Mounts), extraMounts...)
	return rustic.Run(ctx, dockerClient, password, command, repository.Environment, mounts, networkMode)
}

var (
	ErrRecoveryKeyNotConfigured = errors.New("recovery key is not configured")
	// ErrBackupSettled reports a checkpointed backup whose own attempt already failed it, so there is nothing to
	// resume; a retry starts a new backup instead.
	ErrBackupSettled = errors.New("the checkpointed backup already failed")

	// RecoveryKeyFormat is 8 hyphenated groups of 6 base32 characters, used verbatim as the Rustic password.
	RecoveryKeyFormat = regexp.MustCompile(`^[A-Z2-7]{6}(-[A-Z2-7]{6}){7}$`)
)

// ValidateRecoveryKey checks that a key has the generated recovery key format.
func ValidateRecoveryKey(key string) error {
	if !RecoveryKeyFormat.MatchString(strings.TrimSpace(key)) {
		return errors.New("enter the generated recovery key (8 groups of 6 characters)")
	}
	return nil
}

// GenerateRecoveryKey creates a random recovery key in RecoveryKeyFormat.
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

// NewRecoveryKeyStore builds the recovery key store.
func NewRecoveryKeyStore(db *database.DB) *RecoveryKeyStore {
	return &RecoveryKeyStore{db: db}
}

// Configured reports whether a recovery key is stored.
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
	return s.load(ctx, RecoveryKeyConfigID)
}

// Set validates and stores the recovery key, encrypted.
func (s *RecoveryKeyStore) Set(ctx context.Context, recoveryKey string) error {
	return s.save(ctx, RecoveryKeyConfigID, recoveryKey)
}

// HoldRunKey keeps a recovery key entered for one backup run, encrypted, until ReleaseRunKey, so the run can
// resume after a restart without it ever entering the run's workflow payload.
func (s *RecoveryKeyStore) HoldRunKey(ctx context.Context, runID, recoveryKey string) error {
	return s.save(ctx, "run:"+runID, recoveryKey)
}

// RunKey returns the key held for a run, or "" when none is.
func (s *RecoveryKeyStore) RunKey(ctx context.Context, runID string) (string, error) {
	key, err := s.load(ctx, "run:"+runID)
	if errors.Is(err, ErrRecoveryKeyNotConfigured) {
		return "", nil
	}
	return key, err
}

// ReleaseRunKey forgets the key held for a run.
func (s *RecoveryKeyStore) ReleaseRunKey(ctx context.Context, runID string) error {
	if err := s.db.WithContext(ctx).Where("id = ?", "run:"+runID).Delete(&SystemBackupRecoveryConfig{}).Error; err != nil {
		return fmt.Errorf("failed to release backup run recovery key: %w", err)
	}
	return nil
}

func (s *RecoveryKeyStore) load(ctx context.Context, id string) (string, error) {
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

func (s *RecoveryKeyStore) save(ctx context.Context, id, recoveryKey string) error {
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

// AcceptedTarget is the progress a manual backup's workflow starts with, so startup recovery protects
// its record from the moment the backup is accepted.
func AcceptedTarget(backupID string) scheduler.TargetOutcome {
	data, _ := json.Marshal(struct {
		BackupID string `json:"backupId"`
	}{backupID})
	return scheduler.TargetOutcome{ResourceType: "backup", ID: "accepted:" + backupID, Status: scheduler.Queued, RecoveryData: data}
}

// AcceptedBackupID returns the backup a manual backup workflow's run owns, from its AcceptedTarget.
func AcceptedBackupID(run scheduler.Run) string {
	index := slices.IndexFunc(run.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
		return strings.HasPrefix(target.ID, "accepted:")
	})
	if index < 0 {
		return ""
	}
	return strings.TrimPrefix(run.Outcome.Targets[index].ID, "accepted:")
}

// lockRepository serializes commands against one repository and returns its unlock func.
func (e *Engine) lockRepository(ctx context.Context, id string) (func(), error) {
	if e == nil {
		return nil, errors.New("backup engine is unavailable")
	}
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

// Run applies updates as the full policy set: it saves them, removes the rest, and reschedules their jobs.
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
	case r.DB == nil:
		persistErr = errors.New("backup policy database is unavailable")
	default:
		persistErr = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for i := range policies {
				if saveErr := tx.Save(&policies[i]).Error; saveErr != nil {
					return saveErr
				}
			}
			for i := range removed {
				if deleteErr := tx.Delete(&removed[i]).Error; deleteErr != nil {
					return deleteErr
				}
			}
			return nil
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

var ErrRemoteRepositoryMissing = errors.New("S3 backup storage is missing")

const RemoteDisabledMessage = "S3 backup storage is missing. Remote backups were disabled; edit the policy to resume."

// RemoteSnapshotChecker memoizes targeted checks within one response budget; each lookup names the
// repository root that holds the snapshot. A nil result means the remote copy could not be verified.
func RemoteSnapshotChecker(ctx context.Context, destinations *s3.S3DestinationService) func(destinationID, root, snapshotID string) *bool {
	checked := make(map[string]*backup.RepositoryObservation)
	deadline := time.Now().Add(10 * time.Second)
	return func(destinationID, root, snapshotID string) *bool {
		if destinations == nil || root == "" || destinationID == "" || snapshotID == "" {
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

// DisableMissingRemote stops a scheduled policy from using S3 storage that went missing: a policy that also backs up
// locally loses S3, an S3-only one is disabled. save clears that field on the stored policy unless it changed since it
// was loaded, and reports whether it did; the loaded flags then follow. It reports whether S3 was turned off.
func DisableMissingRemote(remoteErr error, localEnabled bool, s3Enabled, enabled *bool, save func(field string) (bool, error)) (bool, error) {
	if !errors.Is(remoteErr, ErrRemoteRepositoryMissing) {
		return false, nil
	}
	field, flag := "enabled", enabled
	if localEnabled {
		field, flag = "s3_enabled", s3Enabled
	}
	saved, err := save(field)
	if err != nil || !saved {
		return false, err
	}
	*flag = false
	return true, nil
}

// DisableStoredRemote is DisableMissingRemote's save for a policy table row, which it changes only while the row still
// matches the loaded policy.
func DisableStoredRemote(ctx context.Context, db *database.DB, model any, id, destinationID string, localEnabled bool, field string) (bool, error) {
	result := db.WithContext(ctx).Model(model).
		Where("id = ? AND s3_destination_id = ? AND enabled = ? AND s3_enabled = ? AND local_enabled = ?", id, destinationID, true, true, localEnabled).
		Update(field, false)
	return result.Error == nil && result.RowsAffected > 0, result.Error
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
