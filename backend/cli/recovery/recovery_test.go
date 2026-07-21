package recovery

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	recoverytypes "github.com/getarcaneapp/arcane/backend/v2/internal/recovery"
	"github.com/stretchr/testify/require"
)

func TestFinalizeRestoredBackupInternal(t *testing.T) {
	databaseURL := "file:" + filepath.Join(t.TempDir(), "arcane.db")
	dsn, err := database.ParseSQLiteConnectionString(databaseURL)
	require.NoError(t, err)
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE system_backup_runs (
		id TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL,
		updated_at DATETIME,
		status TEXT NOT NULL,
		size INTEGER NOT NULL DEFAULT 0,
		local_snapshot_id TEXT,
		remote_snapshot_id TEXT,
		s3_destination_id TEXT,
		trigger TEXT,
		destination TEXT,
		policy_id TEXT,
		error TEXT
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE activities (
		id TEXT PRIMARY KEY, updated_at DATETIME, status TEXT NOT NULL, resource_type TEXT,
		progress INTEGER, step TEXT, latest_message TEXT, error TEXT, started_at DATETIME NOT NULL,
		ended_at DATETIME, duration_ms INTEGER
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO system_backup_runs (id, created_at, status, error) VALUES
		('older', '2026-01-01T00:00:00Z', 'running', ''),
		('selected', '2026-01-02T00:00:00Z', 'running', '')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO activities (id, status, resource_type, started_at) VALUES ('activity-1', 'running', 'system_backup', '2026-01-02T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.NoError(t, finalizeRestoredBackupInternal(context.Background(), databaseURL, "selected", "activity-1", recoverytypes.RestoreRequest{
		BackupID: "request-id", RemoteSnapshotID: "snapshot-1", S3DestinationID: "destination-1", Size: 1234,
		SafetyBackup: &recoverytypes.SafetyBackup{
			ID: "safety", LocalSnapshotID: "safety-snapshot", Size: 4321,
			CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		},
	}))
	db, err = sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	var selectedStatus, olderStatus, remoteSnapshotID, destinationID, activityStatus string
	var size int64
	require.NoError(t, db.QueryRow(`SELECT status, size, remote_snapshot_id, s3_destination_id FROM system_backup_runs WHERE id = 'selected'`).Scan(&selectedStatus, &size, &remoteSnapshotID, &destinationID))
	require.NoError(t, db.QueryRow(`SELECT status FROM system_backup_runs WHERE id = 'older'`).Scan(&olderStatus))
	require.Equal(t, "succeeded", selectedStatus)
	require.EqualValues(t, 1234, size)
	require.Equal(t, "snapshot-1", remoteSnapshotID)
	require.Equal(t, "destination-1", destinationID)
	require.Equal(t, "running", olderStatus)
	require.NoError(t, db.QueryRow(`SELECT status FROM activities WHERE id = 'activity-1'`).Scan(&activityStatus))
	require.Equal(t, "success", activityStatus)
	var safetyStatus, safetyTrigger, safetyDestination, safetySnapshotID string
	var safetySize int64
	require.NoError(t, db.QueryRow(`SELECT status, trigger, destination, local_snapshot_id, size FROM system_backup_runs WHERE id = 'safety'`).Scan(
		&safetyStatus, &safetyTrigger, &safetyDestination, &safetySnapshotID, &safetySize,
	))
	require.Equal(t, "succeeded", safetyStatus)
	require.Equal(t, "safety", safetyTrigger)
	require.Equal(t, "local", safetyDestination)
	require.Equal(t, "safety-snapshot", safetySnapshotID)
	require.EqualValues(t, 4321, safetySize)
}

func TestFinalizeRestoredBackupInternalFallsBackForLegacyManifest(t *testing.T) {
	databaseURL := "file:" + filepath.Join(t.TempDir(), "arcane.db")
	dsn, err := database.ParseSQLiteConnectionString(databaseURL)
	require.NoError(t, err)
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE system_backup_runs (
		id TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL,
		updated_at DATETIME,
		status TEXT NOT NULL,
		size INTEGER NOT NULL DEFAULT 0,
		local_snapshot_id TEXT,
		remote_snapshot_id TEXT,
		s3_destination_id TEXT,
		trigger TEXT,
		destination TEXT,
		policy_id TEXT,
		error TEXT
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE activities (
		id TEXT PRIMARY KEY, updated_at DATETIME, status TEXT NOT NULL, resource_type TEXT,
		progress INTEGER, step TEXT, latest_message TEXT, error TEXT, started_at DATETIME NOT NULL,
		ended_at DATETIME, duration_ms INTEGER
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO system_backup_runs (id, created_at, status, error) VALUES
		('older', '2026-01-01T00:00:00Z', 'running', ''),
		('newest', '2026-01-02T00:00:00Z', 'running', '')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO activities (id, status, resource_type, started_at) VALUES ('legacy-activity', 'running', 'system_backup', '2026-01-02T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.NoError(t, finalizeRestoredBackupInternal(context.Background(), databaseURL, "", "", recoverytypes.RestoreRequest{BackupID: "missing-discovery-id"}))
	db, err = sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	var status, activityStatus string
	require.NoError(t, db.QueryRow(`SELECT status FROM system_backup_runs WHERE id = 'newest'`).Scan(&status))
	require.Equal(t, "succeeded", status)
	require.NoError(t, db.QueryRow(`SELECT status FROM activities WHERE id = 'legacy-activity'`).Scan(&activityStatus))
	require.Equal(t, "success", activityStatus)
}
