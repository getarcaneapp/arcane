package recovery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/cli/upgrade"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	recoverytypes "github.com/getarcaneapp/arcane/backend/v2/internal/recovery"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	rusticruntime "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/rustic"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/moby/moby/api/pkg/stdcopy"
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
	RunE:   runRestore,
}

func init() {
	RestoreCmd.Flags().StringVar(&requestPath, "request", "/app/data/.arcane-recovery-request.json", "Prepared recovery request")
}

func runRestore(_ *cobra.Command, _ []string) error {
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
	if err := restoreSnapshot(ctx, dockerClient, request); err != nil {
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
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	update := func(where string, arguments ...any) (bool, error) {
		queryArguments := make([]any, 0, 4+len(arguments))
		queryArguments = append(queryArguments, request.Size, request.LocalSnapshotID, request.RemoteSnapshotID, request.S3DestinationID)
		queryArguments = append(queryArguments, arguments...)
		//nolint:gosec // where is selected only from fixed internal SQL predicates below
		result, updateErr := db.ExecContext(ctx, `
UPDATE system_backup_runs
SET status = 'succeeded', error = '', updated_at = CURRENT_TIMESTAMP,
	size = CASE WHEN size = 0 THEN ? ELSE size END,
	local_snapshot_id = CASE WHEN COALESCE(local_snapshot_id, '') = '' THEN ? ELSE local_snapshot_id END,
	remote_snapshot_id = CASE WHEN COALESCE(remote_snapshot_id, '') = '' THEN ? ELSE remote_snapshot_id END,
	s3_destination_id = CASE WHEN COALESCE(s3_destination_id, '') = '' THEN ? ELSE s3_destination_id END
WHERE `+where+` AND status = 'running'`, queryArguments...)
		if updateErr != nil {
			return false, updateErr
		}
		changed, rowsErr := result.RowsAffected()
		return changed > 0, rowsErr
	}
	finalized := false
	for _, backupID := range []string{manifestBackupID, request.BackupID} {
		if strings.TrimSpace(backupID) == "" {
			continue
		}
		if changed, updateErr := update("id = ?", backupID); updateErr != nil {
			return updateErr
		} else if changed {
			finalized = true
			break
		}
	}
	if !finalized {
		if _, err = update("id = (SELECT id FROM system_backup_runs WHERE status = 'running' ORDER BY created_at DESC LIMIT 1)"); err != nil {
			return err
		}
	}
	if err := preserveSafetyBackupInternal(ctx, db, request.SafetyBackup); err != nil {
		return err
	}
	return finalizeRestoredActivityInternal(ctx, db, manifestActivityID)
}

func preserveSafetyBackupInternal(ctx context.Context, db *sql.DB, backup *recoverytypes.SafetyBackup) error {
	if backup == nil || strings.TrimSpace(backup.ID) == "" || strings.TrimSpace(backup.LocalSnapshotID) == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO system_backup_runs (
	id, size, created_at, updated_at, status, trigger, destination,
	local_snapshot_id, remote_snapshot_id, s3_destination_id, policy_id, error
) VALUES (?, ?, ?, CURRENT_TIMESTAMP, 'succeeded', 'safety', 'local', ?, '', '', '', '')
ON CONFLICT(id) DO UPDATE SET
	size = excluded.size,
	updated_at = CURRENT_TIMESTAMP,
	status = 'succeeded',
	trigger = 'safety',
	destination = 'local',
	local_snapshot_id = excluded.local_snapshot_id,
	error = ''`, backup.ID, backup.Size, backup.CreatedAt, backup.LocalSnapshotID)
	return err
}

func finalizeRestoredActivityInternal(ctx context.Context, db *sql.DB, activityID string) error {
	where, arguments := "id = ?", []any{activityID}
	if strings.TrimSpace(activityID) == "" {
		where = "id = (SELECT id FROM activities WHERE status = 'running' AND resource_type = 'system_backup' ORDER BY started_at DESC LIMIT 1)"
		arguments = nil
	}
	//nolint:gosec // where is selected only from the fixed ID and latest-running predicates above
	_, err := db.ExecContext(ctx, `
UPDATE activities
SET status = 'success', progress = 100, step = 'System backup completed',
	latest_message = 'Arcane system backup created successfully', error = NULL,
	ended_at = CURRENT_TIMESTAMP,
	duration_ms = CAST((julianday(CURRENT_TIMESTAMP) - julianday(started_at)) * 86400000 AS INTEGER),
	updated_at = CURRENT_TIMESTAMP
WHERE `+where+` AND status = 'running'`, arguments...)
	return err
}

func restoreSnapshot(ctx context.Context, dockerClient *client.Client, request recoverytypes.RestoreRequest) error {
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
	environment := append([]string{}, request.RepositoryEnvironment...)
	environment = append(environment, "RUSTIC_PASSWORD="+request.RecoveryKey, "RUSTIC_NO_PROGRESS=true", "RUSTIC_LOG_LEVEL=error")
	mounts := append([]mount.Mount{}, request.RepositoryMounts...)
	request.AppDataMount.Target = "/restore"
	request.AppDataMount.ReadOnly = false
	mounts = append(mounts, request.AppDataMount)
	hostConfig := volumehelper.HostConfig(rusticImage, nil, mounts)
	hostConfig.AutoRemove = false
	snapshotPath := strings.TrimSpace(request.SnapshotPath)
	if snapshotPath == "" {
		snapshotPath = "/"
	}
	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:  rusticImage,
			Cmd:    []string{"restore", "--delete", request.SnapshotID + ":" + snapshotPath, "/restore"},
			Env:    environment,
			Labels: volumehelper.Labels(),
		},
		HostConfig: hostConfig,
	})
	if err != nil {
		return fmt.Errorf("create Rustic restore container: %w", err)
	}
	defer func() { _, _ = dockerClient.ContainerRemove(ctx, created.ID, volumehelper.RemoveOptions()) }()
	if _, err := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start Rustic restore container: %w", err)
	}
	wait := dockerClient.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var status container.WaitResponse
	select {
	case err := <-wait.Error:
		if err != nil {
			return fmt.Errorf("wait for Rustic restore: %w", err)
		}
	case status = <-wait.Result:
	}
	if status.StatusCode == 0 {
		return nil
	}
	logs, err := dockerClient.ContainerLogs(ctx, created.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return fmt.Errorf("rustic restore exited with code %d", status.StatusCode)
	}
	defer func() { _ = logs.Close() }()
	var stdout, stderr bytes.Buffer
	_, _ = stdcopy.StdCopy(&stdout, &stderr, logs)
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		message = strings.TrimSpace(stdout.String())
	}
	slog.Error("Rustic system restore failed", "status", status.StatusCode)
	return fmt.Errorf("rustic restore exited with code %d: %s", status.StatusCode, message)
}
