package policies

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
)

func noLatestRun(context.Context, string) (*volume.BackupEntry, error) {
	return nil, nil
}

func newVolumeAdmissionForTest(t testing.TB) *runs.Admission {
	t.Helper()
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	return gate
}

type volumeBackupPolicyScheduler struct {
	submitted []scheduler.Request
	jobs      map[string]scheduler.Job
}

func (s *volumeBackupPolicyScheduler) AddJob(_ context.Context, job scheduler.Job) error {
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

func TestVolumeBackupPolicy_UpdateRegistersIndependentJobsAndSettings(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-schedule?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}))
	db := &database.DB{DB: gormDB}
	jobScheduler := &volumeBackupPolicyScheduler{jobs: make(map[string]scheduler.Job)}
	service := NewService(Dependencies{DB: db, LatestRun: noLatestRun})
	gate := newVolumeAdmissionForTest(t)
	require.NoError(t, service.SetScheduler(t.Context(), jobScheduler, gate))

	collection, err := service.UpdateBackupPolicies(t.Context(), "app-data", []volume.UpdateBackupPolicy{
		{Enabled: true, Schedule: "0 */15 * * * *", RetentionCount: 5, StopContainers: true, LocalEnabled: true},
		{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 30, LocalEnabled: true},
	})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	require.Equal(t, 5, collection.Policies[0].RetentionCount)
	require.True(t, collection.Policies[0].StopContainers)
	require.Equal(t, 30, collection.Policies[1].RetentionCount)
	require.Len(t, jobScheduler.jobs, 2)

	firstID := collection.Policies[0].ID
	collection, err = service.UpdateBackupPolicies(t.Context(), "app-data", []volume.UpdateBackupPolicy{{
		ID: firstID, Schedule: "0 */30 * * * *", RetentionCount: 9, LocalEnabled: true,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.Equal(t, firstID, collection.Policies[0].ID)
	require.Equal(t, 9, collection.Policies[0].RetentionCount)
	require.Empty(t, jobScheduler.jobs)
}

func TestVolumeBackupPolicy_GetReturnsLastRunForEachPolicy(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-last-runs?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}))
	first := &VolumeBackupPolicy{VolumeName: "app-data", Schedule: "0 0 2 * * *", LocalEnabled: true}
	second := &VolumeBackupPolicy{VolumeName: "app-data", Schedule: "0 0 14 * * *", LocalEnabled: true}
	require.NoError(t, gormDB.Create(first).Error)
	require.NoError(t, gormDB.Create(second).Error)
	latest := map[string]*volume.BackupEntry{
		first.ID:  {VolumeName: "app-data", PolicyID: first.ID, Status: backup.VolumeBackupStatusSucceeded},
		second.ID: {VolumeName: "app-data", PolicyID: second.ID, Status: backup.VolumeBackupStatusFailed},
	}

	service := NewService(Dependencies{
		DB: &database.DB{DB: gormDB},
		LatestRun: func(_ context.Context, policyID string) (*volume.BackupEntry, error) {
			return latest[policyID], nil
		},
	})
	collection, err := service.GetBackupPolicies(t.Context(), "app-data")
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	require.Equal(t, "succeeded", collection.Policies[0].LastRun.Status)
	require.Equal(t, "failed", collection.Policies[1].LastRun.Status)
}

func TestVolumeBackupPolicy_UpdateUsesSelectedS3Destination(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-s3-secret?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}, &s3.S3Destination{}))
	db := &database.DB{DB: gormDB}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-volume-backups-32bytes",
		Environment:   "test",
	})
	encryptedSecret, err := crypto.Encrypt("destination-s3-secret")
	require.NoError(t, err)
	destination := &s3.S3Destination{
		Name:            "Offsite",
		Bucket:          "volume-backups",
		Region:          "us-east-1",
		AccessKeyID:     "destination-access-key",
		SecretAccessKey: encryptedSecret,
		UseSSL:          true,
	}
	require.NoError(t, gormDB.Create(destination).Error)
	service := NewService(Dependencies{
		DB:             db,
		S3Destinations: s3.NewS3DestinationService(db, nil),
		LatestRun:      noLatestRun,
	})
	collection, err := service.UpdateBackupPolicies(t.Context(), "app-data", []volume.UpdateBackupPolicy{{
		Schedule:        "0 0 2 * * *",
		RetentionCount:  7,
		S3Enabled:       true,
		S3DestinationID: destination.ID,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	policy := collection.Policies[0]
	require.False(t, policy.LocalEnabled)
	require.True(t, policy.S3Enabled)
	require.True(t, policy.S3Available)
	require.Equal(t, destination.ID, policy.S3DestinationID)
	require.Equal(t, "Offsite", policy.S3DestinationName)
	require.Equal(t, "volume-backups", policy.S3Bucket)

	var stored VolumeBackupPolicy
	require.NoError(t, gormDB.Where("volume_name = ?", "app-data").First(&stored).Error)
	require.False(t, stored.LocalEnabled)
	require.True(t, stored.S3Enabled)
	require.Equal(t, destination.ID, stored.S3DestinationID)
}

func TestBackupPolicyDestinationLookupFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&VolumeBackupPolicy{}, &s3.S3Destination{}))
	policy := &VolumeBackupPolicy{VolumeName: "app-data", Schedule: "0 0 2 * * *", S3DestinationID: "missing"}
	require.NoError(t, db.Create(policy).Error)
	service := NewService(Dependencies{DB: &database.DB{DB: db}, S3Destinations: s3.NewS3DestinationService(&database.DB{DB: db}, nil), LatestRun: noLatestRun})
	for _, drop := range []bool{false, true} {
		if drop {
			require.NoError(t, db.Migrator().DropTable(&s3.S3Destination{}))
		}
		collection, getBackupPoliciesErr := service.GetBackupPolicies(t.Context(), "app-data")
		require.NoError(t, getBackupPoliciesErr)
		require.Len(t, collection.Policies, 1)
		require.False(t, collection.S3Available)
		require.Empty(t, collection.Policies[0].S3DestinationName)
		require.Empty(t, collection.Policies[0].S3Bucket)
	}
}

func (s *volumeBackupPolicyScheduler) Submit(_ context.Context, request scheduler.Request) (scheduler.Run, error) {
	s.submitted = append(s.submitted, request)
	return scheduler.Run{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: scheduler.Queued}, nil
}
