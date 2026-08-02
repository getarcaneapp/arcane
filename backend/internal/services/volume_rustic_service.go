package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const (
	volumeRusticImage          = "ghcr.io/rustic-rs/rustic:v0.11.2"
	volumeRusticRepositoryPath = "/repository/volumes"
)

type rusticRepositoryInternal struct {
	environment []string
	mounts      []mount.Mount
}

type rusticSnapshotInternal struct {
	ID      string `json:"id"`
	Summary struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
	} `json:"summary"`
}

func (s *VolumeService) rusticPasswordInternal() string {
	sum := sha256.Sum256([]byte("arcane-volume-backups:" + s.encryptionKey))
	return hex.EncodeToString(sum[:])
}

func (s *VolumeService) localRusticRepositoryInternal(ctx context.Context, dockerClient *client.Client, readOnly bool) (rusticRepositoryInternal, error) {
	storage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/repository", readOnly)
	if err != nil {
		return rusticRepositoryInternal{}, err
	}
	return rusticRepositoryInternal{
		environment: []string{"RUSTIC_REPOSITORY=" + volumeRusticRepositoryPath},
		mounts:      []mount.Mount{storage.mount},
	}, nil
}

func (s *VolumeService) remoteRusticRepositoryInternal(ctx context.Context, destinationID string) (rusticRepositoryInternal, error) {
	if s.s3Destinations == nil {
		return rusticRepositoryInternal{}, errors.New("S3 backup service is unavailable")
	}
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
	instanceID := strings.TrimSpace(s.settingsService.GetSettingsConfig().InstanceID.Value)
	if instanceID == "" {
		return rusticRepositoryInternal{}, errors.New("arcane instance ID is unavailable")
	}
	repositoryRoot := path.Join("/", cfg.S3Prefix, "arcane-volume-backups", instanceID)
	environment := []string{
		"RUSTIC_REPOSITORY=opendal:s3",
		"RUSTIC_REPO_OPT_BUCKET=" + cfg.S3Bucket,
		"RUSTIC_REPO_OPT_ROOT=" + repositoryRoot,
		"AWS_ACCESS_KEY_ID=" + cfg.S3AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + cfg.S3SecretAccessKey,
		"AWS_EC2_METADATA_DISABLED=true",
	}
	if endpoint != "" {
		environment = append(environment, "RUSTIC_REPO_OPT_ENDPOINT="+endpoint)
	}
	region := strings.TrimSpace(cfg.S3Region)
	if region == "" {
		region = "auto"
	}
	environment = append(environment, "RUSTIC_REPO_OPT_REGION="+region)
	environment = append(environment, "AWS_REGION="+region)
	if !cfg.S3ForcePathStyle {
		environment = append(environment, "RUSTIC_REPO_OPT_ENABLE_VIRTUAL_HOST_STYLE=true")
	}
	return rusticRepositoryInternal{environment: environment}, nil
}

func (s *VolumeService) ensureRusticImageInternal(ctx context.Context, dockerClient *client.Client) error {
	if _, err := dockerClient.ImageInspect(ctx, volumeRusticImage); err == nil {
		return nil
	}
	if s.imageService == nil {
		return errors.New("image service is unavailable")
	}
	if err := s.imageService.PullImage(ctx, volumeRusticImage, io.Discard, systemUser, nil); err != nil {
		return fmt.Errorf("failed to pull Rustic image: %w", err)
	}
	return nil
}

func (s *VolumeService) runRusticInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	s.rusticMu.Lock()
	defer s.rusticMu.Unlock()
	if err := s.ensureRusticImageInternal(ctx, dockerClient); err != nil {
		return "", err
	}
	environment := append([]string{}, repository.environment...)
	environment = append(environment,
		"RUSTIC_PASSWORD="+password,
		"RUSTIC_NO_PROGRESS=true",
		"RUSTIC_LOG_LEVEL=error",
	)
	mounts := append([]mount.Mount{}, repository.mounts...)
	mounts = append(mounts, extraMounts...)
	hostConfig := volumehelper.HostConfig(volumeRusticImage, nil, mounts)
	hostConfig.AutoRemove = false
	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:  volumeRusticImage,
			Cmd:    command,
			Env:    environment,
			Labels: volumehelper.Labels(),
		},
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create Rustic container: %w", err)
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), created.ID, volumehelper.RemoveOptions())
	}()
	if _, err := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start Rustic container: %w", err)
	}
	wait := dockerClient.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var status container.WaitResponse
	select {
	case err := <-wait.Error:
		if err != nil {
			return "", fmt.Errorf("failed to wait for Rustic container: %w", err)
		}
	case status = <-wait.Result:
	}
	logs, err := dockerClient.ContainerLogs(ctx, created.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", fmt.Errorf("failed to read Rustic output: %w", err)
	}
	defer func() { _ = logs.Close() }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logs); err != nil {
		return "", fmt.Errorf("failed to decode Rustic output: %w", err)
	}
	if status.StatusCode != 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("rustic exited with code %d: %s", status.StatusCode, message)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (s *VolumeService) createRusticSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, sourceVolumeName, label string) (rusticSnapshotInternal, error) {
	output, err := s.runRusticInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(),
		[]string{"backup", "--init", "--json", "--as-path", "/", "--host", "arcane", "--label", label, "/volume"},
		mount.Mount{Type: mount.TypeVolume, Source: sourceVolumeName, Target: "/volume", ReadOnly: true},
	)
	if err != nil {
		return rusticSnapshotInternal{}, err
	}
	var snapshot rusticSnapshotInternal
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		return rusticSnapshotInternal{}, fmt.Errorf("failed to decode Rustic snapshot: %w", err)
	}
	if snapshot.ID == "" {
		return rusticSnapshotInternal{}, errors.New("rustic did not return a snapshot ID")
	}
	return snapshot, nil
}

func (s *VolumeService) rusticRepositoryForBackupInternal(ctx context.Context, dockerClient *client.Client, backup *models.VolumeBackup) (rusticRepositoryInternal, string, error) {
	if backup.LocalSnapshotID != "" {
		repository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
		return repository, backup.LocalSnapshotID, err
	}
	if backup.RemoteSnapshotID != "" {
		repository, err := s.remoteRusticRepositoryInternal(ctx, backup.S3DestinationID)
		return repository, backup.RemoteSnapshotID, err
	}
	return rusticRepositoryInternal{}, "", errors.New("volume backup has no Rustic snapshot")
}

func (s *VolumeService) restoreRusticSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, snapshotID, volumeName string, deleteExtra bool, sourcePath, destinationPath string) error {
	command := []string{"restore"}
	if deleteExtra {
		command = append(command, "--delete")
	}
	source := snapshotID + ":/"
	if sourcePath != "" {
		source = snapshotID + ":/" + strings.TrimPrefix(sourcePath, "/")
	}
	if destinationPath == "" {
		destinationPath = "/volume"
	}
	command = append(command, source, destinationPath)
	_, err := s.runRusticInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(), command,
		mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume"},
	)
	return err
}

func (s *VolumeService) forgetRusticSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, snapshotID string) error {
	_, err := s.runRusticInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(), []string{"forget", "--prune", snapshotID})
	return err
}

//nolint:gocognit,gocyclo // backup orchestration keeps cleanup and persistence transitions in one transaction-like flow
func (s *VolumeService) CreateBackup(ctx context.Context, volumeName string, user models.User, trigger models.VolumeBackupTrigger, request volumetypes.CreateBackupRequest) (_ *models.VolumeBackup, err error) {
	if _, loaded := s.runningBackups.LoadOrStore(volumeName, struct{}{}); loaded {
		return nil, ErrVolumeBackupAlreadyRunning
	}
	defer s.runningBackups.Delete(volumeName)
	destination, policyID := request.Destination, request.PolicyID
	if trigger == "" {
		trigger = models.VolumeBackupTriggerManual
	}
	if destination != "" && destination != volumetypes.BackupDestinationLocal && destination != volumetypes.BackupDestinationS3 && destination != volumetypes.BackupDestinationLocalS3 {
		return nil, errors.New("invalid volume backup destination")
	}
	var policy *models.VolumeBackupPolicy
	if trigger != models.VolumeBackupTriggerSafety {
		policy, err = s.loadVolumeBackupPolicyInternal(ctx, volumeName, policyID)
		if err != nil {
			return nil, err
		}
		if policyID != "" && policy == nil {
			return nil, errors.New("volume backup policy not found")
		}
	}
	localEnabled, s3Enabled, s3DestinationID := true, false, strings.TrimSpace(request.S3DestinationID)
	if policy != nil {
		localEnabled, s3Enabled, s3DestinationID = policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID
	}
	if trigger == models.VolumeBackupTriggerSafety {
		localEnabled, s3Enabled = true, false
	} else if destination != "" {
		localEnabled = destination != volumetypes.BackupDestinationS3
		s3Enabled = destination != volumetypes.BackupDestinationLocal
	}
	if !localEnabled && !s3Enabled {
		return nil, errors.New("select at least one volume backup destination")
	}
	if s3Enabled && strings.TrimSpace(s3DestinationID) == "" {
		return nil, errors.New("select an S3 destination for the volume backup")
	}
	if s3Enabled {
		if s.s3Destinations == nil {
			return nil, errors.New("S3 backup destinations are unavailable")
		}
		if _, err := s.s3Destinations.configurationInternal(ctx, s3DestinationID); err != nil {
			return nil, errors.New("select a valid S3 destination for the volume backup")
		}
	} else {
		s3DestinationID = ""
	}
	switch {
	case localEnabled && s3Enabled:
		destination = volumetypes.BackupDestinationLocalS3
	case s3Enabled:
		destination = volumetypes.BackupDestinationS3
	default:
		destination = volumetypes.BackupDestinationLocal
	}
	backup := &models.VolumeBackup{
		VolumeName: volumeName, CreatedAt: time.Now(), Status: models.VolumeBackupStatusRunning,
		Trigger: trigger, Destination: destination, S3DestinationID: s3DestinationID, PolicyID: policyID,
	}
	backup.ID = fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.NewString()[:8])
	if err := s.db.WithContext(ctx).Create(backup).Error; err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			backup.Status, backup.Error = models.VolumeBackupStatusFailed, err.Error()
		} else {
			backup.Status, backup.Error = models.VolumeBackupStatusSucceeded, ""
		}
		if saveErr := s.db.WithContext(context.WithoutCancel(ctx)).Save(backup).Error; saveErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to save volume backup result: %w", saveErr))
		}
	}()
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return backup, err
	}
	var stopped []container.Summary
	containersStopped := false
	if policy != nil && policy.StopContainers {
		stopped, err = s.stopRunningContainersForBackupInternal(ctx, dockerClient, volumeName, user)
		containersStopped = len(stopped) > 0
		defer func() {
			if containersStopped {
				_, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
				err = errors.Join(err, restartErr)
			}
		}()
		if err != nil {
			return backup, err
		}
	}
	if localEnabled {
		repository, repoErr := s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if repoErr != nil {
			return backup, repoErr
		}
		snapshot, snapshotErr := s.createRusticSnapshotInternal(ctx, dockerClient, repository, volumeName, volumeName)
		if snapshotErr != nil {
			return backup, fmt.Errorf("failed to create local Rustic snapshot: %w", snapshotErr)
		}
		backup.LocalSnapshotID = snapshot.ID
		backup.Size = snapshot.Summary.TotalBytesProcessed
	}
	if s3Enabled {
		repository, repoErr := s.remoteRusticRepositoryInternal(ctx, s3DestinationID)
		if repoErr != nil {
			return backup, repoErr
		}
		snapshot, snapshotErr := s.createRusticSnapshotInternal(ctx, dockerClient, repository, volumeName, volumeName)
		if snapshotErr != nil {
			return backup, fmt.Errorf("failed to create S3 Rustic snapshot: %w", snapshotErr)
		}
		backup.RemoteSnapshotID = snapshot.ID
		if backup.Size == 0 {
			backup.Size = snapshot.Summary.TotalBytesProcessed
		}
	}
	if containersStopped {
		stopped, err = s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return backup, err
		}
	}
	if trigger != models.VolumeBackupTriggerSafety && policy != nil && policy.RetentionCount > 0 {
		if err := s.applyVolumeBackupRetentionInternal(ctx, policy.ID, policy.RetentionCount); err != nil {
			return backup, fmt.Errorf("failed to apply volume backup retention: %w", err)
		}
	}
	metadata := models.JSON{"action": "backup_create", "backup_id": backup.ID, "size": backup.Size, "destination": backup.Destination}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupCreate, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup create event", "volume", volumeName, "error", logErr)
	}
	return backup, nil
}

func (s *VolumeService) UploadBackup(ctx context.Context, backupID, s3DestinationID string) (*models.VolumeBackup, error) {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return nil, err
	}
	if backup.Status != models.VolumeBackupStatusSucceeded || backup.LocalSnapshotID == "" {
		return nil, errors.New("only successful local volume backups can be uploaded")
	}
	if backup.RemoteSnapshotID != "" {
		return nil, errors.New("volume backup has already been uploaded")
	}
	if strings.TrimSpace(s3DestinationID) == "" {
		return nil, errors.New("select an S3 destination for the upload")
	}
	if s.s3Destinations == nil {
		return nil, errors.New("S3 backup destinations are unavailable")
	}
	if _, err := s.s3Destinations.configurationInternal(ctx, s3DestinationID); err != nil {
		return nil, errors.New("select a valid S3 destination for the upload")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	temporaryVolume := "arcane-rustic-copy-" + uuid.NewString()
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: temporaryVolume, Labels: volumehelper.Labels()}); err != nil {
		return nil, fmt.Errorf("failed to create temporary Rustic copy volume: %w", err)
	}
	defer func() {
		_, _ = dockerClient.VolumeRemove(context.WithoutCancel(ctx), temporaryVolume, client.VolumeRemoveOptions{Force: true})
	}()
	localRepository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	if err := s.restoreRusticSnapshotInternal(ctx, dockerClient, localRepository, backup.LocalSnapshotID, temporaryVolume, true, "", ""); err != nil {
		return nil, fmt.Errorf("failed to load local Rustic snapshot for upload: %w", err)
	}
	remoteRepository, err := s.remoteRusticRepositoryInternal(ctx, s3DestinationID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.createRusticSnapshotInternal(ctx, dockerClient, remoteRepository, temporaryVolume, backup.VolumeName)
	if err != nil {
		return nil, fmt.Errorf("failed to upload Rustic snapshot to S3: %w", err)
	}
	backup.RemoteSnapshotID = snapshot.ID
	backup.S3DestinationID = s3DestinationID
	backup.Destination = volumetypes.BackupDestinationLocalS3
	if err := s.db.WithContext(ctx).Save(&backup).Error; err != nil {
		return nil, fmt.Errorf("failed to save uploaded volume backup: %w", err)
	}
	return &backup, nil
}

func (s *VolumeService) DeleteBackup(ctx context.Context, backupID string, user *models.User) error {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	var deleteErr error
	if backup.LocalSnapshotID != "" {
		repository, repoErr := s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if repoErr == nil {
			repoErr = s.forgetRusticSnapshotInternal(ctx, dockerClient, repository, backup.LocalSnapshotID)
		}
		if repoErr != nil {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete local Rustic snapshot: %w", repoErr))
		} else {
			backup.LocalSnapshotID = ""
		}
	}
	if backup.RemoteSnapshotID != "" {
		repository, repoErr := s.remoteRusticRepositoryInternal(ctx, backup.S3DestinationID)
		if repoErr == nil {
			repoErr = s.forgetRusticSnapshotInternal(ctx, dockerClient, repository, backup.RemoteSnapshotID)
		}
		if repoErr != nil {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete S3 Rustic snapshot: %w", repoErr))
		} else {
			backup.RemoteSnapshotID = ""
			backup.S3DestinationID = ""
		}
	}
	if deleteErr != nil {
		switch {
		case backup.LocalSnapshotID != "" && backup.RemoteSnapshotID != "":
			backup.Destination = volumetypes.BackupDestinationLocalS3
		case backup.LocalSnapshotID != "":
			backup.Destination = volumetypes.BackupDestinationLocal
		case backup.RemoteSnapshotID != "":
			backup.Destination = volumetypes.BackupDestinationS3
		}
		backup.Error = deleteErr.Error()
		if saveErr := s.db.WithContext(ctx).Save(&backup).Error; saveErr != nil {
			deleteErr = errors.Join(deleteErr, saveErr)
		}
		return deleteErr
	}
	if err := s.db.WithContext(ctx).Delete(&backup).Error; err != nil {
		return fmt.Errorf("failed to delete volume backup record: %w", err)
	}
	actingUser := user
	if actingUser == nil {
		actingUser = &systemUser
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupDelete, backup.VolumeName, backup.VolumeName, actingUser.ID, actingUser.Username, "0", models.JSON{"action": "backup_delete", "backup_id": backupID}); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup delete event", "volume", backup.VolumeName, "error", logErr)
	}
	return nil
}

func (s *VolumeService) RestoreBackup(ctx context.Context, volumeName, backupID string, user models.User) (err error) {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	if backup.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	repository, snapshotID, err := s.rusticRepositoryForBackupInternal(ctx, dockerClient, &backup)
	if err != nil {
		return err
	}
	stopped, err := s.stopRunningContainersForBackupInternal(ctx, dockerClient, volumeName, user)
	containersStopped := len(stopped) > 0
	defer func() {
		if containersStopped {
			_, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
			err = errors.Join(err, restartErr)
		}
	}()
	if err != nil {
		return err
	}
	preBackup, err := s.CreateBackup(ctx, volumeName, user, models.VolumeBackupTriggerSafety, volumetypes.CreateBackupRequest{Destination: volumetypes.BackupDestinationLocal})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}
	if err := s.restoreRusticSnapshotInternal(ctx, dockerClient, repository, snapshotID, volumeName, true, "", ""); err != nil {
		return fmt.Errorf("failed to restore Rustic snapshot: %w", err)
	}
	if containersStopped {
		stopped, err = s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return err
		}
	}
	metadata := models.JSON{"action": "backup_restore", "backup_id": backupID, "pre_restore_backupId": preBackup.ID}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore event", "volume", volumeName, "error", logErr)
	}
	return nil
}

func (s *VolumeService) ListBackupFiles(ctx context.Context, backupID string) ([]string, error) {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return nil, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	repository, snapshotID, err := s.rusticRepositoryForBackupInternal(ctx, dockerClient, &backup)
	if err != nil {
		return nil, err
	}
	output, err := s.runRusticInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(), []string{"ls", "--json", "--recursive", snapshotID + ":/"})
	if err != nil {
		return nil, fmt.Errorf("failed to list Rustic snapshot: %w", err)
	}
	var files []string
	if err := json.Unmarshal([]byte(output), &files); err != nil {
		return nil, fmt.Errorf("failed to decode Rustic file list: %w", err)
	}
	return files, nil
}

func (s *VolumeService) BackupHasPath(ctx context.Context, backupID, filePath string) (bool, error) {
	cleaned, err := s.sanitizeBackupPathInternal(filePath)
	if err != nil {
		return false, err
	}
	files, err := s.ListBackupFiles(ctx, backupID)
	if err != nil {
		return false, err
	}
	for _, candidate := range files {
		if strings.TrimPrefix(candidate, "/") == cleaned {
			return true, nil
		}
	}
	return false, nil
}

func (s *VolumeService) RestoreBackupFiles(ctx context.Context, volumeName, backupID string, paths []string, user models.User) (err error) {
	if len(paths) == 0 {
		return errors.New("no paths provided")
	}
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	if backup.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	cleanedPaths := make([]string, 0, len(paths))
	for _, requestedPath := range paths {
		cleaned, cleanErr := s.sanitizeBackupPathInternal(requestedPath)
		if cleanErr != nil {
			return cleanErr
		}
		cleanedPaths = append(cleanedPaths, cleaned)
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	repository, snapshotID, err := s.rusticRepositoryForBackupInternal(ctx, dockerClient, &backup)
	if err != nil {
		return err
	}
	stopped, err := s.stopRunningContainersForBackupInternal(ctx, dockerClient, volumeName, user)
	containersStopped := len(stopped) > 0
	defer func() {
		if containersStopped {
			_, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
			err = errors.Join(err, restartErr)
		}
	}()
	if err != nil {
		return err
	}
	preBackup, err := s.CreateBackup(ctx, volumeName, user, models.VolumeBackupTriggerSafety, volumetypes.CreateBackupRequest{Destination: volumetypes.BackupDestinationLocal})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}
	for _, cleaned := range cleanedPaths {
		if err := s.restoreRusticSnapshotInternal(ctx, dockerClient, repository, snapshotID, volumeName, false, cleaned, path.Join("/volume", cleaned)); err != nil {
			return fmt.Errorf("failed to restore %s from Rustic snapshot: %w", cleaned, err)
		}
	}
	if containersStopped {
		stopped, err = s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return err
		}
	}
	metadata := models.JSON{"action": "backup_restore_files", "backup_id": backupID, "pre_restore_backupId": preBackup.ID, "paths_count": len(cleanedPaths)}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupRestoreFiles, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore files event", "volume", volumeName, "error", logErr)
	}
	return nil
}
