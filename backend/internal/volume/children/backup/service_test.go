package backup

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/scheduler/flow/flowtest"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/volume/children/backup/children/policies"
	"getarcane.dev/types/v3"
)

type (
	volumeBackupPolicyScheduler struct {
		jobs map[string]types.SchedulerJob
	}
)

func newVolumeBackupTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := database.Initialize(t.Context(), "file:"+name+"?mode=memory&cache=shared", database.MigrationOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db.DB
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
	gormDB := newVolumeBackupTestDB(t, "volume-backup-retention-failed")

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
		Schedule:       "0 2 * * *",
		RetentionCount: 7,
		LocalEnabled:   true,
	}
	require.NoError(t, gormDB.Create(policy).Error)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error {
		return nil
	})
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	jobScheduler := &volumeBackupPolicyScheduler{jobs: make(map[string]types.SchedulerJob)}
	service := NewService(Dependencies{
		DB:             db,
		Engine:         engine,
		AlreadyRunning: errors.New("a backup is already running for this volume"),
		Ctx:            t.Context(),
		Scheduler:      jobScheduler,
		Admission:      gate,
	})
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)
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

	_, err = service.policies.UpdateBackupPolicies(ctx, "app-data", []types.UpdateBackupPolicy{{Schedule: "0 2 * * *", RetentionCount: 7}})
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
	crypto.InitEncryption(crypto.Config{EncryptionKey: "volume-backup-password-test-key-32bytes", Environment: "test"})
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
	require.NoError(t, recoveryKeys.Set(t.Context(), backup.RecoveryKeyConfigID, recoveryKey))
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

func TestOrphanedVolumeBackupIDs(t *testing.T) {
	systemSeries := func(owner string) string {
		return types.SystemVolumePolicyPrefix + owner + ":" + kit.SHA256Hex("app-data")[:16]
	}
	scheduled := types.VolumeBackupTriggerScheduled
	manual := types.VolumeBackupTriggerManual
	candidates := []unmanagedCandidate{
		{ID: "manual", VolumeName: "app-data", Trigger: manual},
		{ID: "volume-policy", VolumeName: "app-data", PolicyID: "policy-1", Trigger: scheduled},
		{ID: "deleted-volume-policy", VolumeName: "app-data", PolicyID: "policy-deleted", Trigger: scheduled},
		{ID: "manual-deleted-volume-policy", VolumeName: "app-data", PolicyID: "policy-deleted", Trigger: manual},
		{ID: "system-policy", VolumeName: "app-data", PolicyID: systemSeries("nightly"), Trigger: scheduled},
		{ID: "system-manual", VolumeName: "app-data", PolicyID: systemSeries("manual"), Trigger: manual},
		{ID: "deleted-system-policy", VolumeName: "app-data", PolicyID: systemSeries("weekly"), Trigger: scheduled},
		{ID: "manual-deleted-system-policy", VolumeName: "app-data", PolicyID: systemSeries("weekly"), Trigger: manual},
		{ID: "removed-volume", VolumeName: "old-data", Trigger: manual},
		{ID: "removed-volume-with-policy", VolumeName: "old-data", PolicyID: systemSeries("nightly"), Trigger: scheduled},
	}

	orphaned := orphanedVolumeBackupIDs(candidates, []string{"app-data"}, []string{"policy-1"}, []string{"manual", "nightly"})

	require.Equal(t, []string{"deleted-volume-policy", "deleted-system-policy", "removed-volume", "removed-volume-with-policy"}, orphaned)
}

func TestDeleteExpiredBackupsLeavesBusyVolumesForTheNextRun(t *testing.T) {
	gormDB := newVolumeBackupTestDB(t, "volume-backup-series-retention-busy")
	db := &database.DB{DB: gormDB}
	seriesID := types.SystemVolumePolicyPrefix + "nightly:" + kit.SHA256Hex("app-data")[:16]
	for i, id := range []string{"oldest", "older", "newest"} {
		require.NoError(t, gormDB.Exec(
			"INSERT INTO volume_backups (id, volume_name, size, created_at, policy_id, status, format, local_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			id, "app-data", 0, time.Now().Add(time.Duration(i-3)*time.Hour), seriesID, types.VolumeBackupStatusSucceeded,
			types.VolumeBackupFormatRustic, "snapshot-"+id,
		).Error)
	}
	locks := &concurrency.KeyedMutex{}
	service := NewService(Dependencies{
		DB:    db,
		Locks: locks,
		Store: Store{Runs: func(ctx context.Context, ids []string) ([]*types.VolumeBackup, error) {
			var entries []*types.VolumeBackup
			return entries, gormDB.WithContext(ctx).Table("volume_backups").Where("id IN ?", ids).Find(&entries).Error
		}},
	})
	unlock := locks.Lock("app-data")
	defer unlock()

	require.NoError(t, service.DeleteExpiredBackups(t.Context(), ExpiredBackupDeletion{
		IncludeRemote: true,
		Expired: func(ctx context.Context, _ []string) ([]string, error) {
			return backup.ExpiredRunIDs(ctx, db, "volume_backups", seriesID, 1)
		},
	}))

	var remaining int64
	require.NoError(t, gormDB.Table("volume_backups").Where("policy_id = ?", seriesID).Count(&remaining).Error)
	require.EqualValues(t, 3, remaining)
}
