package recovery

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kit.getarcane.app/acfs"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/docker/compat"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/cli/upgrade"
	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/config/schema"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/pkg/francis"
	rusticruntime "getarcane.dev/arcane/v3/pkg/rustic"
	"getarcane.dev/types/v3"
)

type arcaneContainer struct {
	stop  func(ctx context.Context) error
	start func(ctx context.Context) error
}

const (
	rusticImage = rusticruntime.DefaultImage
)

var (
	requestPath string

	RestoreCmd = &cobra.Command{
		Use:    "recovery-restore",
		Short:  "Apply a prepared Arcane recovery snapshot",
		Hidden: true,
		RunE:   runRestore,
	}
)

func init() {
	RestoreCmd.Flags().StringVar(&requestPath, "request", "/app/data/.arcane-recovery-request.json", "Prepared recovery request")
}

func runRestore(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	requestFile, err := filepath.Abs(requestPath)
	if err != nil {
		return fmt.Errorf("read recovery request: %w", err)
	}
	data, err := acfs.Host{}.ReadFile(ctx, requestFile)
	if err != nil {
		return fmt.Errorf("read recovery request: %w", err)
	}
	var request types.RecoveryRestoreRequest
	if unmarshalErr := json.Unmarshal(data, &request); unmarshalErr != nil {
		return fmt.Errorf("decode recovery request: %w", unmarshalErr)
	}
	_ = acfs.Host{}.Remove(ctx, requestFile)
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
	arcane := arcaneContainer{
		stop: func(ctx context.Context) error {
			if _, stopErr := dockerClient.ContainerStop(ctx, request.ContainerID, client.ContainerStopOptions{Timeout: new(30)}); stopErr != nil {
				return fmt.Errorf("stop Arcane container: %w", stopErr)
			}
			return nil
		},
		start: func(ctx context.Context) error {
			_, startErr := dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{})
			return startErr
		},
	}
	restart := func() { _ = arcane.start(ctx) }
	rustic := func(ctx context.Context, stage types.RecoveryRestoreStage) error {
		trust, trustErr := rusticruntime.TrustMounts(ctx, dockerClient, "/app/data", inspect.Container.Mounts)
		if trustErr != nil {
			return trustErr
		}
		command := []string{"restore", "--delete"}
		for _, glob := range stage.Globs {
			command = append(command, "--glob", glob)
		}
		command = append(command, stage.SnapshotID+":"+stage.SourcePath, stage.Target.Path)
		mounts := slices.Concat(stage.Repository.Mounts, stage.Target.Mounts, trust)
		_, runErr := rusticruntime.Run(ctx, dockerClient, request.RecoveryKey, command, stage.Repository.Environment, mounts, container.NetworkMode(request.NetworkMode))
		return runErr
	}
	if restoreErr := restoreData(ctx, request, rustic, arcane); restoreErr != nil {
		return restoreErr
	}
	if !request.ProjectsIncluded {
		slog.WarnContext(ctx, "the restored system backup did not include the projects directory; the current projects directory was left untouched")
	}
	manifest, err := readRestoredManifest(ctx, "/app/data")
	if err != nil {
		restart()
		return err
	}
	if config.DirectAgentRequested(func(name string) string {
		return manifest.Environment[name]
	}) && manifest.Environment["ARCANE_AGENT"] != "true" {
		restart()
		return fmt.Errorf("refusing to recover Arcane container: %w", config.ErrDirectAgentRemoved)
	}
	// Projects were restored into the current directory, so the recovered
	// configuration must keep pointing there rather than at the backup-time path.
	manifest.Environment[settings.EnvName("projectsDirectory", "")] = cmp.Or(request.ProjectsSetting, manifest.Environment[settings.EnvName("projectsDirectory", "")])
	if name := settings.EnvName("projectsDirectories", ""); manifest.Environment[name] != "" {
		manifest.Environment[name] = cmp.Or(request.ProjectsDirectoriesSetting, manifest.Environment[name])
	}
	if finalizeRestoredBackupErr := finalizeRestoredBackup(ctx, manifest.Environment, manifest.BackupID, manifest.ActivityID, request); finalizeRestoredBackupErr != nil {
		return fmt.Errorf("finalize restored system backup: %w", finalizeRestoredBackupErr)
	}
	if upgradeContainerErr := upgrade.UpgradeContainer(ctx, dockerClient, inspect.Container, request.ContainerImage, "", manifest.Environment); upgradeContainerErr != nil {
		return fmt.Errorf("recreate Arcane container with recovered configuration: %w", upgradeContainerErr)
	}
	return nil
}

// readRestoredManifest reads and removes the recovery manifest a restore put back in directory.
func readRestoredManifest(ctx context.Context, directory string) (types.RecoveryManifest, error) {
	root := acfs.Root{Path: directory}
	manifestData, err := root.ReadFile(ctx, "/.arcane-recovery.json")
	if err != nil {
		return types.RecoveryManifest{}, fmt.Errorf("read restored recovery manifest: %w", err)
	}
	var manifest types.RecoveryManifest
	if err = json.Unmarshal(manifestData, &manifest); err != nil {
		return types.RecoveryManifest{}, fmt.Errorf("decode restored recovery manifest: %w", err)
	}
	_ = root.Remove(ctx, "/.arcane-recovery.json")
	if manifest.FormatVersion < 1 || manifest.FormatVersion > types.RecoveryManifestFormatVersion || len(manifest.Environment) == 0 {
		return types.RecoveryManifest{}, errors.New("unsupported or incomplete Arcane recovery manifest")
	}
	manifest.Environment = schema.ManifestEnvironment(manifest)
	return manifest, nil
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
		AgentEnabled:  environment["ARCANE_AGENT"] == "true",
	})
	if err != nil {
		return err
	}
	crypto.InitEncryption(crypto.Config{
		EncryptionKey: encryptionKey,
		Environment:   environment["ARCANE_ENVIRONMENT"],
	})
	if finalizeRestoredRunErr := finalizeRestoredRun(db, manifestBackupID, request); finalizeRestoredRunErr != nil {
		return finalizeRestoredRunErr
	}
	if strings.TrimSpace(request.RecoveryKey) != "" {
		if setRecoveryKeyErr := backup.NewRecoveryKeyStore(&database.DB{DB: db}).Set(ctx, backup.RecoveryKeyConfigID, request.RecoveryKey); setRecoveryKeyErr != nil {
			return setRecoveryKeyErr
		}
	}
	if preserveSafetyBackupErr := preserveSafetyBackup(db, request.SafetyBackup); preserveSafetyBackupErr != nil {
		return preserveSafetyBackupErr
	}
	// The restored database keeps the current project folders and discovers later project backups on its next start.
	for key, value := range map[string]string{
		"projectsDirectory":             request.ProjectsSetting,
		"projectsDirectories":           request.ProjectsDirectoriesSetting,
		"projectBackupDiscoveryPending": "true",
	} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		operationErr := db.Model(&settings.SettingVariable{}).
			Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.Assignments(map[string]any{"value": value})}).
			Create(map[string]any{"key": key, "value": value}).Error
		if operationErr != nil {
			return operationErr
		}
	}
	if finalizeRestoredActivityErr := finalizeRestoredActivity(db, manifestActivityID); finalizeRestoredActivityErr != nil {
		return finalizeRestoredActivityErr
	}
	if failRestoredProjectRestoresErr := failRestoredProjectRestores(db); failRestoredProjectRestoresErr != nil {
		return failRestoredProjectRestoresErr
	}
	return francis.ClearRestoredHosts(ctx, databaseURL)
}

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

// failRestoredProjectRestores fails the project restores the system backup caught running.
func failRestoredProjectRestores(db *gorm.DB) error {
	var entries []activity.Activity
	if err := db.Where("status = ?", types.ActivityStatusRunning).Find(&entries).Error; err != nil {
		return err
	}
	const message = "Interrupted by a system restore; the project was left as it is"
	for _, entry := range entries {
		if action, _ := entry.Metadata["action"].(string); action != "restore_project_backup" {
			continue
		}
		now := time.Now().UTC()
		if err := db.Model(&activity.Activity{}).Where("id = ?", entry.ID).Updates(map[string]any{
			"status": types.ActivityStatusFailed, "latest_message": message, "error": message,
			"ended_at": now, "duration_ms": now.Sub(entry.StartedAt).Milliseconds(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// restoreData stops Arcane and applies the restore stages through the shared restore runner, with the pre-restore
// system backup as its safety backup. A failure before live data changed starts Arcane again; a later one rolls back to
// the pre-restore backup and starts Arcane only when that succeeds. On success Arcane stays stopped for the caller to
// recreate with the recovered configuration.
func restoreData(
	ctx context.Context,
	request types.RecoveryRestoreRequest,
	rustic func(context.Context, types.RecoveryRestoreStage) error,
	arcane arcaneContainer,
) error {
	steps := backup.RestoreSteps{
		Subject: "Arcane",
		Report: func(ctx context.Context, message string) {
			slog.InfoContext(ctx, "System restore step", "step", message)
		},
		Stop:   func(ctx context.Context, _ backup.RestoreState) error { return arcane.stop(ctx) },
		Deploy: arcane.start,
		Safety: func(context.Context) (string, error) {
			if request.SafetyBackup == nil || len(request.RollbackStages) == 0 {
				return "", errors.New("no pre-restore system backup is available for rollback")
			}
			return request.SafetyBackup.ID, nil
		},
		InUse:  nil,
		Create: nil,
		Write: func(ctx context.Context, safetyID string) error {
			if safetyID == "" {
				touched, err := runStages(ctx, request.Stages, rustic)
				if err != nil && !touched {
					return backup.UnchangedError{Err: err}
				}
				return err
			}
			if _, err := runStages(ctx, request.RollbackStages, rustic); err != nil {
				return fmt.Errorf("restore the pre-restore system backup: %w", err)
			}
			rollback, err := readRestoredManifest(ctx, "/app/data")
			if err == nil {
				err = francis.ClearRestoredHosts(ctx, rollback.Environment["ARCANE_DATABASE_URL"])
			}
			if err != nil {
				return fmt.Errorf("clear restored actor ownership: %w", err)
			}
			return nil
		},
		Verify:  nil,
		Remove:  nil,
		Discard: nil,
	}
	// Arcane ran when the restore was requested; after a successful restore the caller recreates it instead.
	state := backup.RestoreState{Redeploy: new(false), SafetyID: "", Create: nil, MayStop: true, Running: true}
	if err := steps.Apply(ctx, state); err != nil {
		return fmt.Errorf("system restore failed: %w", err)
	}
	return nil
}

// runStages applies stages in order and reports whether live data changed: a staged stage changes it only once its swap
// applies, a direct stage as soon as Rustic starts.
func runStages(ctx context.Context, stages []types.RecoveryRestoreStage, rustic func(context.Context, types.RecoveryRestoreStage) error) (bool, error) {
	touched := false
	for _, stage := range stages {
		if len(stage.Target.Mounts) == 0 || strings.TrimSpace(stage.Target.Path) == "" {
			return touched, fmt.Errorf("restore stage for %s has no destination", stage.SourcePath)
		}
		if stage.Swap == nil {
			touched = true
			if err := rustic(ctx, stage); err != nil {
				slog.ErrorContext(ctx, "Rustic system restore stage failed", "source", stage.SourcePath, "error", err)
				return touched, fmt.Errorf("restore %s: %w", stage.SourcePath, err)
			}
			continue
		}
		directory := stage.Swap.Directory
		if err := backup.RemoveSwapLeftovers(ctx, directory); err != nil {
			return touched, fmt.Errorf("remove an earlier restore's staging folders: %w", err)
		}
		if err := rustic(ctx, stage); err != nil {
			slog.ErrorContext(ctx, "Rustic system restore stage failed", "source", stage.SourcePath, "error", err)
			return touched, errors.Join(fmt.Errorf("restore %s: %w", stage.SourcePath, err), backup.RemoveSwapLeftovers(context.WithoutCancel(ctx), directory))
		}
		plan, err := backup.PlanSwap(ctx, *stage.Swap)
		if err != nil {
			return touched, errors.Join(fmt.Errorf("check the restored data: %w", err), backup.RemoveSwapLeftovers(context.WithoutCancel(ctx), directory))
		}
		err = plan.Apply(ctx)
		if errors.Is(err, backup.ErrSwapNotApplied) {
			return touched, errors.Join(fmt.Errorf("swap the restored data into %s: %w", directory, err), backup.RemoveSwapLeftovers(context.WithoutCancel(ctx), directory))
		}
		touched = true
		if err != nil {
			return touched, fmt.Errorf("swap the restored data into %s: %w", directory, err)
		}
	}
	return touched, nil
}
