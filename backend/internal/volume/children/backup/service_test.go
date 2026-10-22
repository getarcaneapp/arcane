package backup

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume/children/backup/children/policies"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
)

func applyVolumeBackupMigrations(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	for _, name := range []string{"032_add_volume_backups.sql", "073_add_backup_support.sql", "087_add_volume_backup_remote_instance.sql"} {
		migration, err := os.ReadFile("../../../../resources/migrations/sqlite/" + name)
		require.NoError(t, err)
		require.NoError(t, gormDB.Exec(strings.Split(string(migration), "-- +goose Down")[0]).Error)
	}
}

type volumeBackupPolicyScheduler struct {
	jobs map[string]types.SchedulerJob
}

func (s *volumeBackupPolicyScheduler) AddJob(_ context.Context, job types.SchedulerJob) error {
	s.jobs[job.Name()] = job
	return nil
}

func (s *volumeBackupPolicyScheduler) RemoveJob(_ context.Context, name string) {
	delete(s.jobs, name)
}

func (s *volumeBackupPolicyScheduler) HasJob(name string) bool {
	_, ok := s.jobs[name]
	return ok
}

func (s *volumeBackupPolicyScheduler) Submit(_ context.Context, request types.SchedulerRequest) (types.SchedulerRun, error) {
	return types.SchedulerRun{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: types.RunStatusQueued}, nil
}

func TestVolumeBackupPolicy_RetentionCountsFailedRunsSeparately(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-retention-failed?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	applyVolumeBackupMigrations(t, gormDB)

	policyID := "policy-1"
	require.NoError(t, gormDB.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, policy_id, status, local_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"succeeded-run", "app-data", 0, time.Now().Add(-time.Hour), policyID, types.VolumeBackupStatusSucceeded, "snapshot-1",
	).Error)
	require.NoError(t, gormDB.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, policy_id, status) VALUES (?, ?, ?, ?, ?, ?)",
		"failed-run", "app-data", 0, time.Now(), policyID, types.VolumeBackupStatusFailed,
	).Error)
	require.NoError(t, gormDB.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, policy_id, status, local_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"failed-snap-new", "app-data", 0, time.Now().Add(-2*time.Hour), policyID, types.VolumeBackupStatusFailed, "snapshot-2",
	).Error)
	require.NoError(t, gormDB.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, policy_id, status, local_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"failed-snap-old", "app-data", 0, time.Now().Add(-3*time.Hour), policyID, types.VolumeBackupStatusFailed, "snapshot-3",
	).Error)

	expired, err := backup.ExpiredRunIDs(t.Context(), &database.DB{DB: gormDB}, "volume_backups", policyID, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"failed-snap-old"}, expired)
}

func TestVolumeBackupPolicy_ScheduledRunCreatesActivity(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-scheduled-activity?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&policies.VolumeBackupPolicy{}, &activity.Activity{}, &activity.ActivityMessage{}))
	db := &database.DB{DB: gormDB}
	policy := &policies.VolumeBackupPolicy{
		VolumeName:     "app-data",
		Enabled:        true,
		Schedule:       "0 0 2 * * *",
		RetentionCount: 7,
		LocalEnabled:   true,
	}
	require.NoError(t, gormDB.Create(policy).Error)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil)
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	service := NewService(Dependencies{DB: db, Engine: engine, AlreadyRunning: errors.New("a backup is already running for this volume")})
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)
	jobScheduler := &volumeBackupPolicyScheduler{jobs: make(map[string]types.SchedulerJob)}
	require.NoError(t, service.SetScheduler(t.Context(), jobScheduler, gate))
	service.RegisterJobsOnStartup(t.Context())
	job, registered := jobScheduler.jobs["volume-backup:"+policy.ID]
	require.True(t, registered)
	// Holding the volume's admission lease skips the scheduled run, which still completes its activity.
	lease, admitted, err := engine.TryAcquireRun(t.Context(), backup.VolumeAdmissionScope, policy.VolumeName)
	require.NoError(t, err)
	require.True(t, admitted)
	defer lease.Release(t.Context())

	outcome, scheduledBackupErr := job.Run(t.Context())
	require.NoError(t, scheduledBackupErr)
	require.Equal(t, types.RunStatusSkipped, outcome.Status)

	var backupActivity activity.Activity
	require.NoError(t, gormDB.Where("resource_type = ?", "volume_backup").First(&backupActivity).Error)
	require.Equal(t, types.ActivityStatusSuccess, backupActivity.Status)
	require.Equal(t, "scheduled_volume_backup", backupActivity.Metadata["action"])
	require.Equal(t, policy.Schedule, backupActivity.Metadata["schedule"])
}

func TestVolumeBackup_ValidationErrors(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-validation?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&policies.VolumeBackupPolicy{}))
	service := NewService(Dependencies{DB: &database.DB{DB: gormDB}})
	ctx := t.Context()

	_, err = service.policies.UpdateBackupPolicies(ctx, "app-data", []types.UpdateBackupPolicy{{Schedule: "not a cron", LocalEnabled: true}})
	require.ErrorContains(t, err, "invalid volume backup schedule")

	_, err = service.policies.UpdateBackupPolicies(ctx, "app-data", []types.UpdateBackupPolicy{{Schedule: "0 0 2 * * *", RetentionCount: 7}})
	require.ErrorContains(t, err, "select at least one volume backup destination")

	_, err = service.StartBackup(ctx, "0", "app-data", types.Actor{}, types.CreateVolumeBackupRequest{Destination: types.VolumeBackupDestination("invalid")})
	require.EqualError(t, err, "invalid volume backup destination")

	_, err = service.StartBackup(ctx, "0", "app-data", types.Actor{}, types.CreateVolumeBackupRequest{Destination: types.VolumeBackupDestinationS3})
	require.EqualError(t, err, "select an S3 destination for the volume backup")
}

func TestVolumeBackupPasswordPrefersRecoveryKey(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-password?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&backup.SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "volume-backup-password-test-key-32bytes", Environment: "test"})
	recoveryKeys := backup.NewRecoveryKeyStore(&database.DB{DB: gormDB})
	service := NewService(Dependencies{
		DB:            &database.DB{DB: gormDB},
		EncryptionKey: "legacy-instance-secret",
		RecoveryKeys:  recoveryKeys,
	})

	password, err := service.volumeBackupPassword(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, kit.SHA256Hex(legacyVolumePasswordSalt+service.deps.EncryptionKey), password)

	recoveryKey := "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"
	require.NoError(t, recoveryKeys.Set(t.Context(), recoveryKey))
	password, err = service.volumeBackupPassword(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, recoveryKey, password)
}

func TestDiscoveredVolumeBackupMapping(t *testing.T) {
	snapshot := backup.DiscoveredSnapshot{ID: "abc123", Label: "app-data", Time: time.Unix(1700000000, 0).UTC()}
	snapshot.Summary.TotalBytesProcessed = 2048

	entry := discoveredVolumeBackup("destination-1", "instance-a", snapshot)
	require.NotNil(t, entry)
	require.Equal(t, "app-data", entry.VolumeName)
	require.Equal(t, "remote-destination-1-instance-a-abc123", entry.ID)
	require.Equal(t, types.VolumeBackupStatusSucceeded, entry.Status)
	require.Equal(t, types.VolumeBackupTriggerManual, entry.Trigger)
	require.Equal(t, types.VolumeBackupDestinationS3, entry.Destination)
	require.Equal(t, types.VolumeBackupFormatRustic, entry.Format)
	require.Equal(t, "instance-a", entry.RemoteInstanceID)
	require.Equal(t, "destination-1", entry.S3DestinationID)
	require.Equal(t, int64(2048), entry.Size)
	require.True(t, entry.CreatedAt.Equal(time.Unix(1700000000, 0).UTC()))

	// Snapshots without a volume label, and system recovery snapshots, never
	// map to a volume backup.
	require.Nil(t, discoveredVolumeBackup("destination-1", "instance-a", backup.DiscoveredSnapshot{ID: "x"}))
	require.Nil(t, discoveredVolumeBackup("destination-1", "instance-a", backup.DiscoveredSnapshot{ID: "x", Label: "arcane-system-recovery"}))

	// A missing snapshot timestamp falls back to now.
	fallback := discoveredVolumeBackup("d", "r", backup.DiscoveredSnapshot{ID: "x", Label: "vol"})
	require.NotNil(t, fallback)
	require.False(t, fallback.CreatedAt.IsZero())
}

func TestDiscoveredSnapshotDecodesRusticLabel(t *testing.T) {
	// Rustic snapshot JSON carries the volume name in the label field written
	// by CreateSnapshot; discovery maps it back to the volume.
	payload := `{"id":"abc123","label":"app-data","time":"2026-09-08T10:00:00Z","summary":{"total_bytes_processed":2048}}`
	var snapshot backup.DiscoveredSnapshot
	require.NoError(t, json.Unmarshal([]byte(payload), &snapshot))
	require.Equal(t, "abc123", snapshot.ID)
	require.Equal(t, "app-data", snapshot.Label)
	require.Equal(t, int64(2048), snapshot.Summary.TotalBytesProcessed)
	require.False(t, snapshot.Time.IsZero())
}
