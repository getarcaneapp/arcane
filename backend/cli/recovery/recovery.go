package recovery

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/cli/upgrade"
	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config/schema"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	rusticruntime "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/rustic"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
)

const rusticImage = rusticruntime.DefaultImage

var requestPath string

var RestoreCmd = &cobra.Command{
	Use:    "recovery-restore",
	Short:  "Apply a prepared Arcane recovery snapshot",
	Hidden: true,
	RunE:   runRestore,
}

func init() {
	RestoreCmd.Flags().StringVar(&requestPath, "request", "/app/data/.arcane-recovery-request.json", "Prepared recovery request")
}

func runRestore(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	data, err := os.ReadFile(requestPath)
	if err != nil {
		return fmt.Errorf("read recovery request: %w", err)
	}
	var request types.RecoveryRestoreRequest
	if unmarshalErr := json.Unmarshal(data, &request); unmarshalErr != nil {
		return fmt.Errorf("decode recovery request: %w", unmarshalErr)
	}
	_ = os.Remove(requestPath)
	if len(request.Stages) == 0 {
		return errors.New("recovery request has no restore stages")
	}
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("connect to Docker: %w", err)
	}
	defer func() { _ = dockerClient.Close() }()
	inspect, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, request.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect Arcane container: %w", err)
	}
	if _, imageInspectErr := dockerClient.ImageInspect(ctx, rusticImage); imageInspectErr != nil {
		reader, pullErr := dockerClient.ImagePull(ctx, rusticImage, client.ImagePullOptions{})
		if pullErr != nil {
			return fmt.Errorf("pull Arcane tools image for Rustic: %w", pullErr)
		}
		if pullErr = docker.RenderJSONMessageStream(reader, nil); pullErr != nil {
			_ = reader.Close()
			return fmt.Errorf("pull Arcane tools image for Rustic: %w", pullErr)
		}
		_ = reader.Close()
	}
	if _, containerStopErr := dockerClient.ContainerStop(ctx, request.ContainerID, client.ContainerStopOptions{Timeout: new(30)}); containerStopErr != nil {
		return fmt.Errorf("stop Arcane container: %w", containerStopErr)
	}
	restart := func() { _, _ = dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{}) }
	if runStagesErr := runStages(ctx, dockerClient, request, request.Stages); runStagesErr != nil {
		// Arcane restarts only when the rollback succeeds; otherwise it stays
		// stopped rather than running with mismatched data and projects.
		runStagesErr = fmt.Errorf("rustic system restore failed: %w", runStagesErr)
		if len(request.RollbackStages) == 0 {
			return errors.Join(runStagesErr, errors.New("no pre-restore system backup is available for rollback; Arcane was left stopped"))
		}
		if rollbackErr := runStages(context.WithoutCancel(ctx), dockerClient, request, request.RollbackStages); rollbackErr != nil {
			return errors.Join(runStagesErr, fmt.Errorf("restoring the pre-restore system backup failed; Arcane was left stopped: %w", rollbackErr))
		}
		rollbackManifest, readErr := os.ReadFile("/app/data/.arcane-recovery.json")
		var rollback types.RecoveryManifest
		if readErr == nil {
			readErr = json.Unmarshal(rollbackManifest, &rollback)
		}
		if readErr == nil {
			readErr = francis.ClearRestoredHosts(context.WithoutCancel(ctx), schema.ManifestEnvironment(rollback)["ARCANE_DATABASE_URL"])
		}
		if readErr != nil {
			return errors.Join(runStagesErr, fmt.Errorf("clear restored actor ownership; Arcane was left stopped: %w", readErr))
		}
		restart()
		return fmt.Errorf("%w; the pre-restore system backup was restored", runStagesErr)
	}
	if !request.ProjectsIncluded {
		slog.WarnContext(ctx, "the restored system backup did not include the projects directory; the current projects directory was left untouched")
	}
	manifestData, err := os.ReadFile("/app/data/.arcane-recovery.json")
	if err != nil {
		restart()
		return fmt.Errorf("read restored recovery manifest: %w", err)
	}
	var manifest types.RecoveryManifest
	if unmarshalErr2 := json.Unmarshal(manifestData, &manifest); unmarshalErr2 != nil {
		restart()
		return fmt.Errorf("decode restored recovery manifest: %w", unmarshalErr2)
	}
	_ = os.Remove("/app/data/.arcane-recovery.json")
	if !slices.Contains([]int{1, 2, types.RecoveryManifestFormatVersion}, manifest.FormatVersion) || len(manifest.Environment) == 0 {
		restart()
		return errors.New("unsupported or incomplete Arcane recovery manifest")
	}
	manifest.Environment = schema.ManifestEnvironment(manifest)
	if config.DirectAgentRequested(func(name string) string {
		return manifest.Environment[name]
	}) && manifest.Environment["ARCANE_EDGE_AGENT"] != "true" {
		restart()
		return fmt.Errorf("refusing to recover Arcane container: %w", config.ErrDirectAgentRemoved)
	}
	// Projects were restored into the current directory, so the recovered
	// configuration must keep pointing there rather than at the backup-time path.
	manifest.Environment[settings.EnvName("projectsDirectory", "")] = cmp.Or(request.ProjectsSetting, manifest.Environment[settings.EnvName("projectsDirectory", "")])
	if finalizeRestoredBackupErr := finalizeRestoredBackup(ctx, manifest.Environment, manifest.BackupID, manifest.ActivityID, request); finalizeRestoredBackupErr != nil {
		return fmt.Errorf("finalize restored system backup: %w", finalizeRestoredBackupErr)
	}
	if upgradeContainerErr := upgrade.UpgradeContainer(ctx, dockerClient, inspect.Container, request.ContainerImage, "", manifest.Environment); upgradeContainerErr != nil {
		return fmt.Errorf("recreate Arcane container with recovered configuration: %w", upgradeContainerErr)
	}
	return nil
}

func finalizeRestoredBackup(ctx context.Context, environment map[string]string, manifestBackupID, manifestActivityID string, request types.RecoveryRestoreRequest) error {
	databaseURL := environment["ARCANE_DATABASE_URL"]
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
	encryptionKey, err := settings.ResolveEncryptionKey(ctx, &database.DB{DB: db}, &config.Config{
		EncryptionKey: environment["ARCANE_ENCRYPTION_KEY"],
		Environment:   config.AppEnvironment(environment["ARCANE_ENVIRONMENT"]),
		EdgeAgent:     environment["ARCANE_EDGE_AGENT"] == "true",
	})
	if err != nil {
		return err
	}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: encryptionKey,
		Environment:   environment["ARCANE_ENVIRONMENT"],
		AgentMode:     environment["ARCANE_EDGE_AGENT"] == "true",
	})
	if finalizeRestoredRunErr := finalizeRestoredRun(db, manifestBackupID, request); finalizeRestoredRunErr != nil {
		return finalizeRestoredRunErr
	}
	if strings.TrimSpace(request.RecoveryKey) != "" {
		if setRecoveryKeyErr := backup.NewRecoveryKeyStore(&database.DB{DB: db}).Set(ctx, request.RecoveryKey); setRecoveryKeyErr != nil {
			return setRecoveryKeyErr
		}
	}
	if preserveSafetyBackupErr := preserveSafetyBackup(db, request.SafetyBackup); preserveSafetyBackupErr != nil {
		return preserveSafetyBackupErr
	}
	// Keep the restored database pointing at the directory projects were restored into.
	if value := strings.TrimSpace(request.ProjectsSetting); value != "" {
		operationErr := db.Model(&settings.SettingVariable{}).
			Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.Assignments(map[string]any{"value": value})}).
			Create(map[string]any{"key": "projectsDirectory", "value": value}).Error
		if operationErr != nil {
			return operationErr
		}
	}
	if finalizeRestoredActivityErr := finalizeRestoredActivity(db, manifestActivityID); finalizeRestoredActivityErr != nil {
		return finalizeRestoredActivityErr
	}
	return francis.ClearRestoredHosts(ctx, databaseURL)
}

// finalizeRestoredRun marks the restored run succeeded, filling only
// the fields the snapshot's database copy did not already carry. Updates use
// explicit column maps because the restored schema may predate the binary's.
func finalizeRestoredRun(db *gorm.DB, manifestBackupID string, request types.RecoveryRestoreRequest) error {
	var run system.SystemBackupRun
	found := false
	for _, backupID := range []string{manifestBackupID, request.BackupID} {
		if strings.TrimSpace(backupID) == "" {
			continue
		}
		err := db.Where("id = ? AND status = ?", backupID, system.SystemBackupStatusRunning).First(&run).Error
		if err == nil {
			found = true
			break
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if !found {
		err := db.Where("status = ?", system.SystemBackupStatusRunning).Order("created_at DESC").First(&run).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	values := map[string]any{"status": system.SystemBackupStatusSucceeded, "error": ""}
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
	return db.Model(&system.SystemBackupRun{}).Where("id = ?", run.ID).Updates(values).Error
}

func preserveSafetyBackup(db *gorm.DB, safetyBackup *types.RecoverySafetyBackup) error {
	if safetyBackup == nil || strings.TrimSpace(safetyBackup.ID) == "" || strings.TrimSpace(safetyBackup.LocalSnapshotID) == "" {
		return nil
	}
	onConflict := map[string]any{
		"size": safetyBackup.Size, "updated_at": time.Now().UTC(), "status": system.SystemBackupStatusSucceeded,
		"trigger": system.SystemBackupTriggerSafety, "destination": types.SystemBackupDestinationLocal,
		"local_snapshot_id": safetyBackup.LocalSnapshotID, "error": "",
	}
	return db.Model(&system.SystemBackupRun{}).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(onConflict)}).
		Create(map[string]any{
			"id": safetyBackup.ID, "size": safetyBackup.Size, "created_at": safetyBackup.CreatedAt, "updated_at": time.Now().UTC(),
			"status":            system.SystemBackupStatusSucceeded,
			"trigger":           system.SystemBackupTriggerSafety,
			"destination":       types.SystemBackupDestinationLocal,
			"local_snapshot_id": safetyBackup.LocalSnapshotID, "remote_snapshot_id": "", "s3_destination_id": "",
			"policy_id": "", "error": "",
		}).Error
}

func finalizeRestoredActivity(db *gorm.DB, activityID string) error {
	var entry activity.Activity
	query := db.Where("status = ?", types.ActivityStatusRunning)
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
		"status": types.ActivityStatusSuccess, "progress": 100, "step": "System backup completed",
		"latest_message": "Arcane system backup created successfully", "error": nil,
		"ended_at": now, "duration_ms": now.Sub(entry.StartedAt).Milliseconds(),
	}).Error
}

// runStages restores each stage in order, replacing the target's
// contents with the snapshot path.
func runStages(ctx context.Context, dockerClient *client.Client, request types.RecoveryRestoreRequest, stages []types.RecoveryRestoreStage) error {
	for _, stage := range stages {
		if len(stage.Target.Mounts) == 0 || strings.TrimSpace(stage.Target.Path) == "" {
			return fmt.Errorf("restore stage for %s has no destination", stage.SourcePath)
		}
		mounts := append([]mount.Mount{}, stage.Repository.Mounts...)
		mounts = append(mounts, stage.Target.Mounts...)
		command := []string{"restore", "--delete", stage.SnapshotID + ":" + stage.SourcePath, stage.Target.Path}
		if _, err := rusticruntime.Run(ctx, dockerClient, request.RecoveryKey, command, stage.Repository.Environment, mounts, container.NetworkMode(request.NetworkMode)); err != nil {
			slog.ErrorContext(ctx, "Rustic system restore stage failed", "source", stage.SourcePath, "error", err)
			return fmt.Errorf("restore %s: %w", stage.SourcePath, err)
		}
	}
	return nil
}
