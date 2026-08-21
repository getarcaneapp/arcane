package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"emperror.dev/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/cli/upgrade"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	recoverytypes "github.com/getarcaneapp/arcane/backend/v2/internal/recovery"
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	rusticruntime "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/rustic"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
)

const rusticImage = rusticruntime.DefaultImage

var requestPath string

var RestoreCmd = &cobra.Command{
	Use:    "recovery-restore",
	Short:  "Apply a prepared Arcane recovery snapshot",
	Hidden: true,
	RunE:   runRestoreInternal,
}

func init() {
	RestoreCmd.Flags().StringVar(&requestPath, "request", "/app/data/.arcane-recovery-request.json", "Prepared recovery request")
}

func runRestoreInternal(_ *cobra.Command, _ []string) error {
	ctx := context.Background()
	data, err := os.ReadFile(requestPath)
	if err != nil {
		return fmt.Errorf("read recovery request: %w", err)
	}
	var request recoverytypes.RestoreRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return fmt.Errorf("decode recovery request: %w", err)
	}
	_ = os.Remove(requestPath)
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("connect to Docker: %w", err)
	}
	defer func() { _ = dockerClient.Close() }()
	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, request.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect Arcane container: %w", err)
	}
	if _, err := dockerClient.ContainerStop(ctx, request.ContainerID, client.ContainerStopOptions{Timeout: new(30)}); err != nil {
		return fmt.Errorf("stop Arcane container: %w", err)
	}
	if err := restoreSnapshotInternal(ctx, dockerClient, request); err != nil {
		_, _ = dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{})
		return err
	}
	manifestData, err := os.ReadFile("/app/data/.arcane-recovery.json")
	if err != nil {
		_, _ = dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{})
		return fmt.Errorf("read restored recovery manifest: %w", err)
	}
	var manifest recoverytypes.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		_, _ = dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{})
		return fmt.Errorf("decode restored recovery manifest: %w", err)
	}
	_ = os.Remove("/app/data/.arcane-recovery.json")
	if manifest.FormatVersion != 1 || len(manifest.Environment) == 0 {
		_, _ = dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{})
		return errors.New("unsupported or incomplete Arcane recovery manifest")
	}
	if err := finalizeRestoredBackupInternal(ctx, manifest.Environment["DATABASE_URL"], manifest.BackupID, manifest.ActivityID, request); err != nil {
		_, _ = dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{})
		return fmt.Errorf("finalize restored system backup: %w", err)
	}
	if err := upgrade.UpgradeContainer(ctx, dockerClient, inspect.Container, request.ContainerImage, manifest.Environment); err != nil {
		return fmt.Errorf("recreate Arcane container with recovered configuration: %w", err)
	}
	return nil
}

func finalizeRestoredBackupInternal(ctx context.Context, databaseURL, manifestBackupID, manifestActivityID string, request recoverytypes.RestoreRequest) error {
	if !strings.HasPrefix(databaseURL, "file:") {
		return errors.New("restored Arcane database is not SQLite")
	}
	dsn, err := database.ParseSQLiteConnectionString(databaseURL)
	if err != nil {
		return err
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	}()
	db = db.WithContext(ctx)
	if err := finalizeRestoredRunInternal(db, manifestBackupID, request); err != nil {
		return err
	}
	if err := preserveSafetyBackupInternal(db, request.SafetyBackup); err != nil {
		return err
	}
	return finalizeRestoredActivityInternal(db, manifestActivityID)
}

// finalizeRestoredRunInternal marks the restored run succeeded, filling only
// the fields the snapshot's database copy did not already carry. Updates use
// explicit column maps because the restored schema may predate the binary's.
func finalizeRestoredRunInternal(db *gorm.DB, manifestBackupID string, request recoverytypes.RestoreRequest) error {
	var run systembackup.SystemBackupRun
	found := false
	for _, backupID := range []string{manifestBackupID, request.BackupID} {
		if strings.TrimSpace(backupID) == "" {
			continue
		}
		err := db.Where("id = ? AND status = ?", backupID, systembackup.SystemBackupStatusRunning).First(&run).Error
		if err == nil {
			found = true
			break
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if !found {
		err := db.Where("status = ?", systembackup.SystemBackupStatusRunning).Order("created_at DESC").First(&run).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	values := map[string]any{"status": systembackup.SystemBackupStatusSucceeded, "error": ""}
	if run.Size == 0 {
		values["size"] = request.Size
	}
	if run.LocalSnapshotID == "" {
		values["local_snapshot_id"] = request.LocalSnapshotID
	}
	if run.RemoteSnapshotID == "" {
		values["remote_snapshot_id"] = request.RemoteSnapshotID
	}
	if run.S3DestinationID == "" {
		values["s3_destination_id"] = request.S3DestinationID
	}
	return db.Model(&systembackup.SystemBackupRun{}).Where("id = ?", run.ID).Updates(values).Error
}

func preserveSafetyBackupInternal(db *gorm.DB, backup *recoverytypes.SafetyBackup) error {
	if backup == nil || strings.TrimSpace(backup.ID) == "" || strings.TrimSpace(backup.LocalSnapshotID) == "" {
		return nil
	}
	onConflict := map[string]any{
		"size": backup.Size, "updated_at": time.Now().UTC(), "status": systembackup.SystemBackupStatusSucceeded,
		"trigger": systembackup.SystemBackupTriggerSafety, "destination": backuptypes.SystemBackupDestinationLocal,
		"local_snapshot_id": backup.LocalSnapshotID, "error": "",
	}
	return db.Model(&systembackup.SystemBackupRun{}).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(onConflict)}).
		Create(map[string]any{
			"id": backup.ID, "size": backup.Size, "created_at": backup.CreatedAt, "updated_at": time.Now().UTC(),
			"status":            systembackup.SystemBackupStatusSucceeded,
			"trigger":           systembackup.SystemBackupTriggerSafety,
			"destination":       backuptypes.SystemBackupDestinationLocal,
			"local_snapshot_id": backup.LocalSnapshotID, "remote_snapshot_id": "", "s3_destination_id": "",
			"policy_id": "", "error": "",
		}).Error
}

func finalizeRestoredActivityInternal(db *gorm.DB, activityID string) error {
	var entry activity.Activity
	query := db.Where("status = ?", activitytypes.StatusRunning)
	if strings.TrimSpace(activityID) != "" {
		query = query.Where("id = ?", activityID)
	} else {
		query = query.Where("resource_type = ?", "system_backup").Order("started_at DESC")
	}
	err := query.First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return db.Model(&activity.Activity{}).Where("id = ?", entry.ID).Updates(map[string]any{
		"status": activitytypes.StatusSuccess, "progress": 100, "step": "System backup completed",
		"latest_message": "Arcane system backup created successfully", "error": nil,
		"ended_at": now, "duration_ms": now.Sub(entry.StartedAt).Milliseconds(),
	}).Error
}

func restoreSnapshotInternal(ctx context.Context, dockerClient *client.Client, request recoverytypes.RestoreRequest) error {
	if _, err := dockerClient.ImageInspect(ctx, rusticImage); err != nil {
		reader, pullErr := dockerClient.ImagePull(ctx, rusticImage, client.ImagePullOptions{})
		if pullErr != nil {
			return fmt.Errorf("pull Arcane tools image for Rustic: %w", pullErr)
		}
		if pullErr = dockerutil.RenderJSONMessageStream(reader, nil); pullErr != nil {
			_ = reader.Close()
			return fmt.Errorf("pull Arcane tools image for Rustic: %w", pullErr)
		}
		_ = reader.Close()
	}
	mounts := append([]mount.Mount{}, request.RepositoryMounts...)
	request.AppDataMount.Target = "/restore"
	request.AppDataMount.ReadOnly = false
	mounts = append(mounts, request.AppDataMount)
	snapshotPath := strings.TrimSpace(request.SnapshotPath)
	if snapshotPath == "" {
		snapshotPath = "/"
	}
	command := []string{"restore", "--delete", request.SnapshotID + ":" + snapshotPath, "/restore"}
	if _, err := rusticruntime.Run(ctx, dockerClient, request.RecoveryKey, command, request.RepositoryEnvironment, mounts, container.NetworkMode(request.NetworkMode)); err != nil {
		slog.Error("Rustic system restore failed", "error", err)
		return fmt.Errorf("rustic system restore failed: %w", err)
	}
	return nil
}
