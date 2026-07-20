package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
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

func TestVolumeBackupPolicy_UpdateRegistersIndependentJobsAndSettings(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-schedule?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.VolumeBackupPolicy{}, &models.VolumeBackup{}))
	db := &database.DB{DB: gormDB}
	scheduler := &volumeBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	service := &VolumeService{db: db}
	service.SetScheduler(context.Background(), scheduler)

	collection, err := service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{
		{Enabled: true, Schedule: "0 */15 * * * *", RetentionCount: 5, StopContainers: true, LocalEnabled: true},
		{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 30, LocalEnabled: true},
	})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	require.Equal(t, 5, collection.Policies[0].RetentionCount)
	require.True(t, collection.Policies[0].StopContainers)
	require.Equal(t, 30, collection.Policies[1].RetentionCount)
	require.Len(t, scheduler.jobs, 2)

	firstID := collection.Policies[0].ID
	collection, err = service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{{
		ID: firstID, Schedule: "0 */30 * * * *", RetentionCount: 9, LocalEnabled: true,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.Equal(t, firstID, collection.Policies[0].ID)
	require.Equal(t, 9, collection.Policies[0].RetentionCount)
	require.Empty(t, scheduler.jobs)
}

func TestVolumeBackupPolicy_GetReturnsLastRunForEachPolicy(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-last-runs?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.VolumeBackupPolicy{}, &models.VolumeBackup{}))
	first := &models.VolumeBackupPolicy{VolumeName: "app-data", Schedule: "0 0 2 * * *", LocalEnabled: true}
	second := &models.VolumeBackupPolicy{VolumeName: "app-data", Schedule: "0 0 14 * * *", LocalEnabled: true}
	require.NoError(t, gormDB.Create(first).Error)
	require.NoError(t, gormDB.Create(second).Error)
	require.NoError(t, gormDB.Create(&models.VolumeBackup{VolumeName: "app-data", PolicyID: first.ID, Status: models.VolumeBackupStatusSucceeded}).Error)
	require.NoError(t, gormDB.Create(&models.VolumeBackup{VolumeName: "app-data", PolicyID: second.ID, Status: models.VolumeBackupStatusFailed}).Error)

	service := &VolumeService{db: &database.DB{DB: gormDB}}
	collection, err := service.GetBackupPolicies(context.Background(), "app-data")
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	require.Equal(t, "succeeded", collection.Policies[0].LastRun.Status)
	require.Equal(t, "failed", collection.Policies[1].LastRun.Status)
}

func TestVolumeBackupPolicy_UpdateRejectsInvalidCron(t *testing.T) {
	service := &VolumeService{}
	_, err := service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{{Schedule: "not a cron", LocalEnabled: true}})
	require.ErrorContains(t, err, "invalid volume backup schedule")
}

func TestVolumeBackupPolicy_ScheduledRunCreatesActivity(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-scheduled-activity?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.VolumeBackupPolicy{}, &models.VolumeBackup{}, &models.Activity{}))
	db := &database.DB{DB: gormDB}
	policy := &models.VolumeBackupPolicy{
		VolumeName:     "app-data",
		Enabled:        true,
		Schedule:       "0 0 2 * * *",
		RetentionCount: 7,
		LocalEnabled:   true,
	}
	require.NoError(t, gormDB.Create(policy).Error)
	service := &VolumeService{db: db, activityService: NewActivityService(db)}
	service.runningBackups.Store(policy.VolumeName, struct{}{})
	defer service.runningBackups.Delete(policy.VolumeName)

	service.buildVolumeBackupJobInternal(policy.ID, policy.Schedule).Run(context.Background())

	var activity models.Activity
	require.NoError(t, gormDB.Where("resource_type = ?", "volume_backup").First(&activity).Error)
	require.Equal(t, models.ActivityStatusFailed, activity.Status)
	require.Equal(t, "scheduled_volume_backup", activity.Metadata["action"])
	require.Equal(t, policy.Schedule, activity.Metadata["schedule"])
}

func TestVolumeBackupPolicy_UpdateRejectsMissingDestination(t *testing.T) {
	service := &VolumeService{}
	_, err := service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{{Schedule: "0 0 2 * * *", RetentionCount: 7}})
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
	collection, err := service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{{
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

	var stored models.VolumeBackupPolicy
	require.NoError(t, gormDB.Where("volume_name = ?", "app-data").First(&stored).Error)
	require.False(t, stored.LocalEnabled)
	require.True(t, stored.S3Enabled)
	require.Equal(t, destination.ID, stored.S3DestinationID)
}

func TestVolumeBackup_CreateRejectsInvalidDestination(t *testing.T) {
	service := &VolumeService{}
	_, err := service.CreateBackup(
		context.Background(),
		"app-data",
		models.User{},
		models.VolumeBackupTriggerManual,
		volumetypes.BackupDestination("invalid"),
		"",
	)
	require.EqualError(t, err, "invalid volume backup destination")
}

func TestVolumeBackup_ListResolvesDestinationName(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-destination-name?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.VolumeBackup{}, &models.S3Destination{}))
	db := &database.DB{DB: gormDB}
	destination := &models.S3Destination{
		Name:            "Offsite",
		Bucket:          "volume-backups",
		Region:          "us-east-1",
		AccessKeyID:     "access-key",
		SecretAccessKey: "encrypted-secret",
	}
	require.NoError(t, gormDB.Create(destination).Error)
	require.NoError(t, gormDB.Create(&models.VolumeBackup{
		VolumeName:      "app-data",
		Status:          models.VolumeBackupStatusSucceeded,
		Trigger:         models.VolumeBackupTriggerManual,
		Destination:     volumetypes.BackupDestinationLocalS3,
		S3DestinationID: destination.ID,
	}).Error)

	service := &VolumeService{db: db, s3Destinations: NewS3DestinationService(db)}
	backups, _, err := service.ListBackupsPaginated(context.Background(), "app-data", pagination.QueryParams{})
	require.NoError(t, err)
	require.Len(t, backups, 1)
	require.Equal(t, volumetypes.BackupDestinationLocalS3, backups[0].Destination)
	require.Equal(t, "Offsite", backups[0].S3DestinationName)
}
