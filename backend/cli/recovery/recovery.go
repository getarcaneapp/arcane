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
	if _, containerStopErr := dockerClient.ContainerStop(ctx, request.ContainerID, client.ContainerStopOptions{Timeout: new(30)}); containerStopErr != nil {
		return fmt.Errorf("stop Arcane container: %w", containerStopErr)
	}
	restart := func() { _, _ = dockerClient.ContainerStart(ctx, request.ContainerID, client.ContainerStartOptions{}) }
	rustic := func(ctx context.Context, stage types.RecoveryRestoreStage) error {
		command := []string{"restore", "--delete"}
		for _, glob := range stage.Globs {
			command = append(command, "--glob", glob)
		}
		command = append(command, stage.SnapshotID+":"+stage.SourcePath, stage.Target.Path)
		mounts := append(slices.Clone(stage.Repository.Mounts), stage.Target.Mounts...)
		_, runErr := rusticruntime.Run(ctx, dockerClient, request.RecoveryKey, command, stage.Repository.Environment, mounts, container.NetworkMode(request.NetworkMode))
		return runErr
	}
	if restoreErr := restoreData(ctx, request, rustic, restart); restoreErr != nil {
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

// readRestoredManifest reads and removes the manifest the restore put back in directory; versions 1 to 4 are supported.
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
	if !slices.Contains([]int{1, 2, types.RecoveryManifestProjectsFormatVersion, types.RecoveryManifestFormatVersion}, manifest.FormatVersion) ||
		len(manifest.Environment) == 0 {
		return types.RecoveryManifest{}, errors.New("unsupported or incomplete Arcane recovery manifest")
	}
	return types.RecoveryManifest{ //exhaustruct:enforce
		CreatedAt:            manifest.CreatedAt,
		Environment:          schema.ManifestEnvironment(manifest),
		ArcaneVersion:        manifest.ArcaneVersion,
		BackupID:             manifest.BackupID,
		ActivityID:           manifest.ActivityID,
		DataPath:             manifest.DataPath,
		ProjectsPath:         manifest.ProjectsPath,
		DatabasePath:         manifest.DatabasePath,
		ExcludedProjectRoots: manifest.ExcludedProjectRoots,
		FormatVersion:        manifest.FormatVersion,
		ProjectsExcluded:     manifest.ProjectsExcluded,
	}, nil
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
	// The restored database keeps the current project folders, which a system restore never moves, and finds the
	// project backups made after the system backup on its next start.
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

// restoreData applies the restore stages. A failure before live data changed restarts Arcane as it was; a later one
// rolls back to the pre-restore backup and restarts Arcane only when that succeeds, so it never runs half restored.
func restoreData(
	ctx context.Context,
	request types.RecoveryRestoreRequest,
	rustic func(context.Context, types.RecoveryRestoreStage) error,
	restart func(),
) error {
	touched, err := runStages(ctx, request.Stages, rustic)
	if err == nil {
		return nil
	}
	err = fmt.Errorf("system restore failed: %w", err)
	if !touched {
		restart()
		return fmt.Errorf("%w; Arcane's data was not changed and Arcane was restarted", err)
	}
	if len(request.RollbackStages) == 0 {
		return errors.Join(err, errors.New("no pre-restore system backup is available for rollback; Arcane was left stopped"))
	}
	if _, rollbackErr := runStages(context.WithoutCancel(ctx), request.RollbackStages, rustic); rollbackErr != nil {
		return errors.Join(err, fmt.Errorf("restoring the pre-restore system backup failed; Arcane was left stopped: %w", rollbackErr))
	}
	rollbackManifest, readErr := acfs.Root{Path: "/app/data"}.ReadFile(context.WithoutCancel(ctx), "/.arcane-recovery.json")
	var rollback types.RecoveryManifest
	if readErr == nil {
		readErr = json.Unmarshal(rollbackManifest, &rollback)
	}
	if readErr == nil {
		readErr = francis.ClearRestoredHosts(context.WithoutCancel(ctx), schema.ManifestEnvironment(rollback)["ARCANE_DATABASE_URL"])
	}
	if readErr != nil {
		return errors.Join(err, fmt.Errorf("clear restored actor ownership; Arcane was left stopped: %w", readErr))
	}
	restart()
	return fmt.Errorf("%w; the pre-restore system backup was restored", err)
}

// runStages applies stages in order and reports whether live data changed. A staged stage restores into a staging folder
// and only changes live data once its swap applies, unless the swap undoes itself; a direct stage changes it as soon as
// Rustic starts.
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
