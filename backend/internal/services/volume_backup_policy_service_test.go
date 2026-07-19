package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

type volumeBackupPolicySchedulerInternal struct {
	jobs map[string]schedulertypes.Job
}

func (s *volumeBackupPolicySchedulerInternal) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.jobs[job.Name()] = job
	return nil
}

func (s *volumeBackupPolicySchedulerInternal) RemoveJob(_ context.Context, name string) {
	delete(s.jobs, name)
}

func (s *volumeBackupPolicySchedulerInternal) HasJob(name string) bool {
	_, ok := s.jobs[name]
	return ok
}

func TestVolumeBackupPolicy_UpdateRegistersAndRemovesJob(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-schedule?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.VolumeBackupPolicy{}, &models.VolumeBackup{}))
	db := &database.DB{DB: gormDB}
	scheduler := &volumeBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	service := &VolumeService{db: db}
	service.SetScheduler(context.Background(), scheduler)

	policy, err := service.UpdateBackupPolicy(context.Background(), "app-data", volumetypes.UpdateBackupPolicy{
		Enabled:        true,
		Schedule:       "0 */15 * * * *",
		RetentionCount: 5,
		StopContainers: true,
		LocalEnabled:   true,
	})
	require.NoError(t, err)
	require.True(t, policy.Enabled)
	require.Equal(t, "0 */15 * * * *", policy.Schedule)
	require.True(t, policy.StopContainers)
	require.Len(t, scheduler.jobs, 1)

	policy, err = service.UpdateBackupPolicy(context.Background(), "app-data", volumetypes.UpdateBackupPolicy{
		Enabled:        false,
		Schedule:       policy.Schedule,
		RetentionCount: policy.RetentionCount,
		StopContainers: policy.StopContainers,
		LocalEnabled:   true,
	})
	require.NoError(t, err)
	require.False(t, policy.Enabled)
	require.Empty(t, scheduler.jobs)
}

func TestVolumeBackupPolicy_UpdateRejectsInvalidCron(t *testing.T) {
	service := &VolumeService{}
	_, err := service.UpdateBackupPolicy(context.Background(), "app-data", volumetypes.UpdateBackupPolicy{Schedule: "not a cron"})
	require.ErrorContains(t, err, "invalid volume backup schedule")
}

func TestVolumeBackupPolicy_UpdateRejectsMissingDestination(t *testing.T) {
	service := &VolumeService{}
	_, err := service.UpdateBackupPolicy(context.Background(), "app-data", volumetypes.UpdateBackupPolicy{
		Schedule:       "0 0 2 * * *",
		RetentionCount: 7,
	})
	require.ErrorContains(t, err, "select at least one volume backup destination")
}

func TestVolumeBackupPolicy_UpdateUsesSelectedS3Destination(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-s3-secret?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.VolumeBackupPolicy{}, &models.VolumeBackup{}, &models.S3Destination{}))
	db := &database.DB{DB: gormDB}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-volume-backups-32bytes",
		Environment:   "test",
	})
	encryptedSecret, err := crypto.Encrypt("destination-s3-secret")
	require.NoError(t, err)
	destination := &models.S3Destination{
		Name:            "Offsite",
		Bucket:          "volume-backups",
		Region:          "us-east-1",
		AccessKeyID:     "destination-access-key",
		SecretAccessKey: encryptedSecret,
		UseSSL:          true,
	}
	require.NoError(t, gormDB.Create(destination).Error)
	service := &VolumeService{
		db:             db,
		s3Destinations: NewS3DestinationService(db),
	}
	policy, err := service.UpdateBackupPolicy(context.Background(), "app-data", volumetypes.UpdateBackupPolicy{
		Schedule:        "0 0 2 * * *",
		RetentionCount:  7,
		S3Enabled:       true,
		S3DestinationID: destination.ID,
	})
	require.NoError(t, err)
	require.False(t, policy.LocalEnabled)
	require.True(t, policy.S3Enabled)
	require.True(t, policy.S3Available)
	require.Equal(t, destination.ID, policy.S3DestinationID)
	require.Equal(t, "volume-backups", policy.S3Bucket)

	var stored models.VolumeBackupPolicy
	require.NoError(t, gormDB.Where("volume_name = ?", "app-data").First(&stored).Error)
	require.False(t, stored.LocalEnabled)
	require.True(t, stored.S3Enabled)
	require.Equal(t, destination.ID, stored.S3DestinationID)
}
