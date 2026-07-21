package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	recoverytypes "github.com/getarcaneapp/arcane/backend/v2/internal/recovery"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/google/uuid"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

const (
	defaultSystemBackupSchedule = "0 0 3 * * *"
	systemRecoveryManifestPath  = "/app/data/.arcane-recovery.json"
	systemRecoveryHelperPath    = "/app/arcane-recovery-helper"
)

var ErrSystemBackupAlreadyRunning = errors.New("an Arcane system backup is already running")

type SystemBackupService struct {
	db              *database.DB
	dockerService   *DockerClientService
	volumeService   *VolumeService
	s3Destinations  *S3DestinationService
	activityService *ActivityService
	config          *config.Config
	scheduler       DynamicScheduler
	lifecycleCtx    context.Context
	running         sync.Mutex
	inProgress      bool
}

type rusticDiscoveredSnapshotInternal struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Summary struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
	} `json:"summary"`
}

func NewSystemBackupService(db *database.DB, dockerService *DockerClientService, volumeService *VolumeService, s3Destinations *S3DestinationService, activityService *ActivityService, cfg *config.Config) *SystemBackupService {
	return &SystemBackupService{db: db, dockerService: dockerService, volumeService: volumeService, s3Destinations: s3Destinations, activityService: activityService, config: cfg}
}

func (s *SystemBackupService) acquireInternal() bool {
	s.running.Lock()
	defer s.running.Unlock()
	if s.inProgress {
		return false
	}
	s.inProgress = true
	return true
}

func (s *SystemBackupService) releaseInternal() {
	s.running.Lock()
	s.inProgress = false
	s.running.Unlock()
}

func validateRecoveryKeyInternal(key string) error {
	if len(strings.TrimSpace(key)) < 16 {
		return errors.New("recovery key must contain at least 16 characters")
	}
	return nil
}

func recoveryHelperExecutableInternal(mounts []containertypes.MountPoint, executablePath string) (string, *mount.Mount, error) {
	executablePath = strings.TrimSpace(executablePath)
	if executablePath == "" {
		return "", nil, errors.New("Arcane executable path is empty")
	}
	executableMount := dockerutil.MountForSubpath(mounts, executablePath, systemRecoveryHelperPath)
	if executableMount == nil {
		return executablePath, nil, nil
	}
	if executableMount.Type != mount.TypeBind {
		return "", nil, errors.New("the Arcane executable must come from the image or a bind mount")
	}
	executableMount.ReadOnly = true
	return systemRecoveryHelperPath, executableMount, nil
}

func (s *SystemBackupService) recoveryEnvironmentInternal() map[string]string {
	result := make(map[string]string)
	var visit func(reflect.Value)
	visit = func(value reflect.Value) {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return
		}
		typeOf := value.Type()
		for i := range value.NumField() {
			fieldType := typeOf.Field(i)
			field := value.Field(i)
			if fieldType.Anonymous {
				visit(field)
				continue
			}
			name := fieldType.Tag.Get("env")
			if name == "" || !field.CanInterface() {
				continue
			}
			if field.Type() == reflect.TypeFor[os.FileMode]() {
				result[name] = fmt.Sprintf("%#o", uint32(field.Interface().(os.FileMode)))
			} else {
				result[name] = fmt.Sprint(field.Interface())
			}
		}
	}
	visit(reflect.ValueOf(s.config))
	return result
}

func (s *SystemBackupService) writeManifestInternal(ctx context.Context, backupID string) error {
	manifest := recoverytypes.Manifest{FormatVersion: 1, ArcaneVersion: config.Version, BackupID: backupID, ActivityID: activitylib.IDFromContext(ctx), CreatedAt: time.Now().UTC(), Environment: s.recoveryEnvironmentInternal()}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode recovery manifest: %w", err)
	}
	if err := os.WriteFile(systemRecoveryManifestPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write recovery manifest: %w", err)
	}
	return nil
}

func (s *SystemBackupService) appDataMountInternal(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error) {
	dataMount, err := dockerutil.MountForCurrentContainerSubpath(ctx, dockerClient, "/app/data", target)
	if err != nil {
		return mount.Mount{}, fmt.Errorf("failed to inspect Arcane data mount: %w", err)
	}
	if dataMount == nil {
		return mount.Mount{}, errors.New("Arcane system backups require /app/data to be mounted into the Arcane container")
	}
	dataMount.ReadOnly = readOnly
	return *dataMount, nil
}

func (s *SystemBackupService) localRepositoryInternal(ctx context.Context, dockerClient *client.Client, readOnly bool) (rusticRepositoryInternal, error) {
	storage, err := s.volumeService.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/repository", readOnly)
	if err != nil {
		return rusticRepositoryInternal{}, err
	}
	return rusticRepositoryInternal{environment: []string{"RUSTIC_REPOSITORY=/repository/system-recovery"}, mounts: []mount.Mount{storage.mount}}, nil
}

func (s *SystemBackupService) remoteRepositoryInternal(ctx context.Context, destinationID string) (rusticRepositoryInternal, error) {
	cfg, err := s.s3Destinations.configurationInternal(ctx, destinationID)
	if err != nil {
		return rusticRepositoryInternal{}, errors.New("the selected S3 backup destination is not configured")
	}
	endpoint := strings.TrimSpace(cfg.S3Endpoint)
	if endpoint != "" && !strings.Contains(endpoint, "://") {
		if cfg.S3UseSSL {
			endpoint = "https://" + endpoint
		} else {
			endpoint = "http://" + endpoint
		}
	}
	root := path.Join("/", cfg.S3Prefix, "arcane-system-recovery")
	environment := []string{
		"RUSTIC_REPOSITORY=opendal:s3", "RUSTIC_REPO_OPT_BUCKET=" + cfg.S3Bucket, "RUSTIC_REPO_OPT_ROOT=" + root,
		"AWS_ACCESS_KEY_ID=" + cfg.S3AccessKeyID, "AWS_SECRET_ACCESS_KEY=" + cfg.S3SecretAccessKey, "AWS_EC2_METADATA_DISABLED=true",
	}
	if endpoint != "" {
		environment = append(environment, "RUSTIC_REPO_OPT_ENDPOINT="+endpoint)
	}
	region := strings.TrimSpace(cfg.S3Region)
	if region == "" {
		region = "auto"
	}
	environment = append(environment, "RUSTIC_REPO_OPT_REGION="+region, "AWS_REGION="+region)
	if !cfg.S3ForcePathStyle {
		environment = append(environment, "RUSTIC_REPO_OPT_ENABLE_VIRTUAL_HOST_STYLE=true")
	}
	return rusticRepositoryInternal{environment: environment}, nil
}

func (s *SystemBackupService) createSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, recoveryKey string) (rusticSnapshotInternal, error) {
	dataMount, err := s.appDataMountInternal(ctx, dockerClient, "/arcane-data", true)
	if err != nil {
		return rusticSnapshotInternal{}, err
	}
	output, err := s.volumeService.runRusticInternal(ctx, dockerClient, repository, recoveryKey,
		[]string{"backup", "--init", "--json", "--as-path", "/", "--host", "arcane", "--label", "arcane-system-recovery", "/arcane-data"}, dataMount)
	if err != nil {
		return rusticSnapshotInternal{}, err
	}
	var snapshot rusticSnapshotInternal
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		return rusticSnapshotInternal{}, fmt.Errorf("failed to decode Rustic snapshot: %w", err)
	}
	if snapshot.ID == "" {
		return rusticSnapshotInternal{}, errors.New("Rustic did not return a snapshot ID")
	}
	return snapshot, nil
}

func systemSnapshotPathInternal(output string) (string, error) {
	var files []string
	if err := json.Unmarshal([]byte(output), &files); err != nil {
		return "", fmt.Errorf("failed to decode system recovery file list: %w", err)
	}
	for _, file := range files {
		if strings.TrimPrefix(file, "/") == ".arcane-recovery.json" {
			return "/", nil
		}
	}
	for _, file := range files {
		if strings.TrimPrefix(file, "/") == "app/data/.arcane-recovery.json" {
			return "/app/data", nil
		}
	}
	return "", errors.New("system recovery snapshot does not contain an Arcane recovery manifest")
}

func (s *SystemBackupService) snapshotPathInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, recoveryKey, snapshotID string) (string, error) {
	output, err := s.volumeService.runRusticInternal(ctx, dockerClient, repository, recoveryKey,
		[]string{"ls", "--json", "--recursive", snapshotID + ":/"})
	if err != nil {
		return "", fmt.Errorf("failed to inspect system recovery snapshot: %w", err)
	}
	return systemSnapshotPathInternal(output)
}

func (s *SystemBackupService) recoveryKeyInternal(ctx context.Context, supplied string) (string, error) {
	if strings.TrimSpace(supplied) != "" {
		if err := validateRecoveryKeyInternal(supplied); err != nil {
			return "", err
		}
		return supplied, nil
	}
	var recoveryConfig models.SystemBackupRecoveryConfig
	err := s.db.WithContext(ctx).Where("id = ?", systemRecoveryConfigID).First(&recoveryConfig).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", errors.New("enter the recovery key or configure one in system backups")
	}
	if err != nil {
		return "", fmt.Errorf("failed to load stored recovery key: %w", err)
	}
	key, err := crypto.Decrypt(recoveryConfig.EncryptedRecoveryKey)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt stored recovery key: %w", err)
	}
	return key, validateRecoveryKeyInternal(key)
}

func (s *SystemBackupService) CreateBackup(ctx context.Context, user models.User, trigger models.VolumeBackupTrigger, request backuptypes.CreateSystemBackupRequest) (_ *models.SystemBackupRun, err error) {
	if !s.acquireInternal() {
		return nil, ErrSystemBackupAlreadyRunning
	}
	defer s.releaseInternal()
	recoveryKey, err := s.recoveryKeyInternal(ctx, request.RecoveryKey)
	if err != nil {
		return nil, err
	}
	localEnabled, s3Enabled, destinationID := true, false, strings.TrimSpace(request.S3DestinationID)
	if request.PolicyID != "" {
		policy, policyErr := s.loadPolicyInternal(ctx, request.PolicyID)
		if policyErr != nil || policy == nil {
			if policyErr != nil {
				return nil, policyErr
			}
			return nil, errors.New("system backup policy not found")
		}
		localEnabled, s3Enabled, destinationID = policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID
	} else {
		localEnabled = request.Destination != backuptypes.SystemBackupDestinationS3
		s3Enabled = request.Destination != backuptypes.SystemBackupDestinationLocal
	}
	if !localEnabled && !s3Enabled {
		return nil, errors.New("select at least one system backup destination")
	}
	if s3Enabled && destinationID == "" {
		return nil, errors.New("select an S3 destination for the system backup")
	}
	destination := backuptypes.SystemBackupDestinationLocal
	if localEnabled && s3Enabled {
		destination = backuptypes.SystemBackupDestinationLocalS3
	} else if s3Enabled {
		destination = backuptypes.SystemBackupDestinationS3
	}
	run := &models.SystemBackupRun{CreatedAt: time.Now().UTC(), Status: models.VolumeBackupStatusRunning, Trigger: trigger, Destination: destination, S3DestinationID: destinationID, PolicyID: request.PolicyID}
	run.ID = "system-" + uuid.NewString()
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			run.Status, run.Error = models.VolumeBackupStatusFailed, err.Error()
		} else {
			run.Status, run.Error = models.VolumeBackupStatusSucceeded, ""
		}
		if saveErr := s.db.WithContext(context.WithoutCancel(ctx)).Save(run).Error; saveErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to save system backup result: %w", saveErr))
		}
	}()
	if !strings.HasPrefix(s.config.DatabaseURL, "file:") {
		return run, errors.New("Arcane system recovery currently requires the SQLite database provider")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return run, err
	}
	if err := s.writeManifestInternal(ctx, run.ID); err != nil {
		return run, err
	}
	defer func() { _ = os.Remove(systemRecoveryManifestPath) }()
	sqlDB, err := s.db.SQLDB()
	if err != nil {
		return run, err
	}
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL)"); err != nil {
		return run, fmt.Errorf("failed to checkpoint Arcane database: %w", err)
	}
	connection, err := sqlDB.Conn(ctx)
	if err != nil {
		return run, fmt.Errorf("failed to reserve Arcane database connection: %w", err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return run, fmt.Errorf("failed to lock Arcane database for backup: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK") }()
	if localEnabled {
		repository, repoErr := s.localRepositoryInternal(ctx, dockerClient, false)
		if repoErr != nil {
			return run, repoErr
		}
		snapshot, snapshotErr := s.createSnapshotInternal(ctx, dockerClient, repository, recoveryKey)
		if snapshotErr != nil {
			return run, fmt.Errorf("failed to create local system recovery snapshot: %w", snapshotErr)
		}
		run.LocalSnapshotID, run.Size = snapshot.ID, snapshot.Summary.TotalBytesProcessed
	}
	if s3Enabled {
		repository, repoErr := s.remoteRepositoryInternal(ctx, destinationID)
		if repoErr != nil {
			return run, repoErr
		}
		snapshot, snapshotErr := s.createSnapshotInternal(ctx, dockerClient, repository, recoveryKey)
		if snapshotErr != nil {
			return run, fmt.Errorf("failed to create S3 system recovery snapshot: %w", snapshotErr)
		}
		run.RemoteSnapshotID = snapshot.ID
		if run.Size == 0 {
			run.Size = snapshot.Summary.TotalBytesProcessed
		}
	}
	return run, nil
}

func (s *SystemBackupService) ListBackups(ctx context.Context, params pagination.QueryParams) ([]backuptypes.SystemBackupRun, pagination.Response, error) {
	var runs []models.SystemBackupRun
	query := s.db.WithContext(ctx).Model(&models.SystemBackupRun{})
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("status LIKE ? OR trigger LIKE ? OR destination LIKE ? OR error LIKE ?", pattern, pattern, pattern, pattern)
	}
	response, err := pagination.PaginateAndSortDB(params, query, &runs)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list system backups: %w", err)
	}
	names := make(map[string]string)
	if s.s3Destinations != nil {
		if destinations, listErr := s.s3Destinations.ListAllS3Destinations(ctx); listErr == nil {
			for _, destination := range destinations {
				names[destination.ID] = destination.Name
			}
		}
	}
	result := make([]backuptypes.SystemBackupRun, len(runs))
	for i := range runs {
		runs[i].S3DestinationName = names[runs[i].S3DestinationID]
		result[i] = runs[i].ToDTO()
	}
	return result, response, nil
}

func (s *SystemBackupService) backupInternal(ctx context.Context, id string) (*models.SystemBackupRun, error) {
	var run models.SystemBackupRun
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *SystemBackupService) DeleteBackup(ctx context.Context, id, recoveryKey string) error {
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return err
	}
	key, err := s.recoveryKeyInternal(ctx, recoveryKey)
	if err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	var deleteErr error
	if run.LocalSnapshotID != "" {
		repository, repoErr := s.localRepositoryInternal(ctx, dockerClient, false)
		if repoErr == nil {
			_, repoErr = s.volumeService.runRusticInternal(ctx, dockerClient, repository, key, []string{"forget", "--prune", run.LocalSnapshotID})
		}
		if repoErr != nil {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete local snapshot: %w", repoErr))
		} else {
			run.LocalSnapshotID = ""
		}
	}
	if run.RemoteSnapshotID != "" {
		repository, repoErr := s.remoteRepositoryInternal(ctx, run.S3DestinationID)
		if repoErr == nil {
			_, repoErr = s.volumeService.runRusticInternal(ctx, dockerClient, repository, key, []string{"forget", "--prune", run.RemoteSnapshotID})
		}
		if repoErr != nil {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete S3 snapshot: %w", repoErr))
		} else {
			run.RemoteSnapshotID, run.S3DestinationID = "", ""
		}
	}
	if deleteErr != nil {
		run.Error = deleteErr.Error()
		_ = s.db.WithContext(ctx).Save(run).Error
		return deleteErr
	}
	return s.db.WithContext(ctx).Delete(run).Error
}

func (s *SystemBackupService) DiscoverRemoteBackups(ctx context.Context, request backuptypes.DiscoverSystemBackupsRequest) (int, error) {
	recoveryKey, err := s.recoveryKeyInternal(ctx, request.RecoveryKey)
	if err != nil {
		return 0, err
	}
	repository, err := s.remoteRepositoryInternal(ctx, request.S3DestinationID)
	if err != nil {
		return 0, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return 0, err
	}
	output, err := s.volumeService.runRusticInternal(ctx, dockerClient, repository, recoveryKey, []string{"snapshots", "--json"})
	if err != nil {
		return 0, fmt.Errorf("failed to open system recovery repository: %w", err)
	}
	snapshots, err := decodeDiscoveredSnapshotsInternal(output)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.ID) == "" {
			continue
		}
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.SystemBackupRun{}).
			Where("remote_snapshot_id = ? AND s3_destination_id = ?", snapshot.ID, request.S3DestinationID).Count(&count).Error; err != nil {
			return created, err
		}
		if count > 0 {
			continue
		}
		createdAt := snapshot.Time
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		run := &models.SystemBackupRun{
			Size: snapshot.Summary.TotalBytesProcessed, CreatedAt: createdAt, Status: models.VolumeBackupStatusSucceeded,
			Trigger: models.VolumeBackupTriggerManual, Destination: backuptypes.SystemBackupDestinationS3,
			RemoteSnapshotID: snapshot.ID, S3DestinationID: request.S3DestinationID,
		}
		run.ID = fmt.Sprintf("remote-%s-%s", request.S3DestinationID, snapshot.ID)
		if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
			return created, fmt.Errorf("failed to save discovered system backup: %w", err)
		}
		created++
	}
	return created, nil
}

func decodeDiscoveredSnapshotsInternal(output string) ([]rusticDiscoveredSnapshotInternal, error) {
	var snapshots []rusticDiscoveredSnapshotInternal
	if err := json.Unmarshal([]byte(output), &snapshots); err != nil {
		return nil, fmt.Errorf("failed to decode system recovery snapshots: %w", err)
	}
	if len(snapshots) == 0 || snapshots[0].ID != "" {
		return snapshots, nil
	}
	var groups []struct {
		Snapshots []rusticDiscoveredSnapshotInternal `json:"snapshots"`
	}
	if err := json.Unmarshal([]byte(output), &groups); err != nil {
		return nil, fmt.Errorf("failed to decode grouped system recovery snapshots: %w", err)
	}
	snapshots = snapshots[:0]
	for _, group := range groups {
		snapshots = append(snapshots, group.Snapshots...)
	}
	return snapshots, nil
}

func (s *SystemBackupService) UploadBackup(ctx context.Context, id string, request backuptypes.UploadSystemBackupRequest) (*models.SystemBackupRun, error) {
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	if run.Status != models.VolumeBackupStatusSucceeded || run.LocalSnapshotID == "" {
		return nil, errors.New("only successful local system backups can be uploaded")
	}
	if run.RemoteSnapshotID != "" {
		return nil, errors.New("system backup has already been uploaded")
	}
	key, err := s.recoveryKeyInternal(ctx, request.RecoveryKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.S3DestinationID) == "" {
		return nil, errors.New("select an S3 destination for the upload")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	temporaryVolume := "arcane-system-recovery-copy-" + uuid.NewString()
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: temporaryVolume, Labels: volumehelper.Labels()}); err != nil {
		return nil, fmt.Errorf("failed to create temporary recovery volume: %w", err)
	}
	defer func() {
		_, _ = dockerClient.VolumeRemove(context.WithoutCancel(ctx), temporaryVolume, client.VolumeRemoveOptions{Force: true})
	}()
	localRepository, err := s.localRepositoryInternal(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	snapshotPath, err := s.snapshotPathInternal(ctx, dockerClient, localRepository, key, run.LocalSnapshotID)
	if err != nil {
		return nil, err
	}
	_, err = s.volumeService.runRusticInternal(ctx, dockerClient, localRepository, key,
		[]string{"restore", "--delete", run.LocalSnapshotID + ":" + snapshotPath, "/arcane-data"},
		mount.Mount{Type: mount.TypeVolume, Source: temporaryVolume, Target: "/arcane-data"})
	if err != nil {
		return nil, fmt.Errorf("failed to load local recovery snapshot: %w", err)
	}
	remoteRepository, err := s.remoteRepositoryInternal(ctx, request.S3DestinationID)
	if err != nil {
		return nil, err
	}
	output, err := s.volumeService.runRusticInternal(ctx, dockerClient, remoteRepository, key,
		[]string{"backup", "--init", "--json", "--as-path", "/", "--host", "arcane", "--label", "arcane-system-recovery", "/arcane-data"},
		mount.Mount{Type: mount.TypeVolume, Source: temporaryVolume, Target: "/arcane-data", ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to upload system recovery snapshot: %w", err)
	}
	var snapshot rusticSnapshotInternal
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil || snapshot.ID == "" {
		return nil, errors.New("failed to decode uploaded Rustic snapshot")
	}
	run.RemoteSnapshotID, run.S3DestinationID = snapshot.ID, request.S3DestinationID
	run.Destination = backuptypes.SystemBackupDestinationLocalS3
	if err := s.db.WithContext(ctx).Save(run).Error; err != nil {
		return nil, fmt.Errorf("failed to save uploaded system backup: %w", err)
	}
	return run, nil
}

func (s *SystemBackupService) RestoreBackup(ctx context.Context, id, recoveryKey string, user models.User) error {
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != models.VolumeBackupStatusSucceeded {
		return errors.New("only successful system backups can be restored")
	}
	key, err := s.recoveryKeyInternal(ctx, recoveryKey)
	if err != nil {
		return err
	}
	// A local safety snapshot is created before the detached helper is allowed to
	// stop Arcane and replace its data.
	safetyBackup, err := s.CreateBackup(ctx, user, models.VolumeBackupTriggerSafety, backuptypes.CreateSystemBackupRequest{
		Destination: backuptypes.SystemBackupDestinationLocal,
		RecoveryKey: key,
	})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", err)
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	containerID, err := cgroup.CurrentContainerID()
	if err != nil {
		return errors.New("Arcane system restore requires Arcane to run in Docker")
	}
	inspectResult, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect Arcane container: %w", err)
	}
	current := inspectResult.Container
	appDataMount := dockerutil.MountForDestination(current.Mounts, "/app/data", "/app/data")
	if appDataMount == nil {
		return errors.New("Arcane system restore requires /app/data to be mounted")
	}
	var repository rusticRepositoryInternal
	var snapshotID string
	if run.LocalSnapshotID != "" {
		repository, err = s.localRepositoryInternal(ctx, dockerClient, true)
		snapshotID = run.LocalSnapshotID
	} else if run.RemoteSnapshotID != "" {
		repository, err = s.remoteRepositoryInternal(ctx, run.S3DestinationID)
		snapshotID = run.RemoteSnapshotID
	} else {
		return errors.New("system backup has no Rustic snapshot")
	}
	if err != nil {
		return err
	}
	snapshotPath, err := s.snapshotPathInternal(ctx, dockerClient, repository, key, snapshotID)
	if err != nil {
		return err
	}
	request := recoverytypes.RestoreRequest{
		BackupID: run.ID, ContainerID: current.ID, ContainerImage: current.Config.Image, SnapshotID: snapshotID, SnapshotPath: snapshotPath, RecoveryKey: key,
		LocalSnapshotID: run.LocalSnapshotID, RemoteSnapshotID: run.RemoteSnapshotID, S3DestinationID: run.S3DestinationID,
		Size: run.Size,
		SafetyBackup: &recoverytypes.SafetyBackup{
			ID: safetyBackup.ID, LocalSnapshotID: safetyBackup.LocalSnapshotID,
			Size: safetyBackup.Size, CreatedAt: safetyBackup.CreatedAt,
		},
		RepositoryEnvironment: repository.environment, RepositoryMounts: repository.mounts, AppDataMount: *appDataMount,
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode recovery request: %w", err)
	}
	requestFile := "/app/data/.arcane-recovery-request.json"
	if err := os.WriteFile(requestFile, requestData, 0o600); err != nil {
		return fmt.Errorf("write recovery request: %w", err)
	}
	cleanupRequest := true
	defer func() {
		if cleanupRequest {
			_ = os.Remove(requestFile)
		}
	}()
	runtimeOptions, err := resolveSystemUpgraderRuntimeOptionsInternal(ctx, s.dockerService.DockerHost(), &current,
		func(ctx context.Context, containerPath string) (string, error) {
			return projects.GetHostPathForContainerPath(ctx, dockerClient, containerPath)
		},
		func() bool { _, err := cgroup.CurrentContainerID(); return err == nil })
	if err != nil {
		return fmt.Errorf("resolve recovery helper runtime: %w", err)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve Arcane executable: %w", err)
	}
	helperExecutable, executableMount, err := recoveryHelperExecutableInternal(current.Mounts, executablePath)
	if err != nil {
		return fmt.Errorf("resolve recovery helper executable: %w", err)
	}
	mounts := append([]mount.Mount{}, runtimeOptions.Mounts...)
	mounts = append(mounts, *appDataMount)
	if executableMount != nil {
		mounts = append(mounts, *executableMount)
	}
	hostConfig := &containertypes.HostConfig{AutoRemove: true, Mounts: mounts, NetworkMode: runtimeOptions.NetworkMode}
	if current.HostConfig != nil {
		hostConfig.SecurityOpt = append([]string{}, current.HostConfig.SecurityOpt...)
		hostConfig.Privileged = current.HostConfig.Privileged
	}
	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image: current.Config.Image, Cmd: []string{helperExecutable, "recovery-restore", "--request", requestFile}, User: "0:0",
			Env:    runtimeOptions.ContainerEnv,
			Labels: map[string]string{"com.getarcaneapp.arcane.recovery": "true", "com.getarcaneapp.arcane": "true"},
		},
		HostConfig: hostConfig,
		Name:       fmt.Sprintf("%s-recovery-%d", strings.TrimPrefix(current.Name, "/"), time.Now().Unix()),
	})
	if err != nil {
		return fmt.Errorf("create recovery helper container: %w", err)
	}
	if _, err := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("start recovery helper container: %w", err)
	}
	cleanupRequest = false
	return nil
}
