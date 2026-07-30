package services

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	sqlite "github.com/libtnb/sqlite"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

func TestValidateRecoveryKeyInternal(t *testing.T) {
	require.Error(t, validateRecoveryKeyInternal("too-short"))
	require.NoError(t, validateRecoveryKeyInternal("correct horse battery staple"))
}

func TestRecoveryHelperExecutableInternal(t *testing.T) {
	path, executableMount, err := recoveryHelperExecutableInternal(nil, "/app/arcane")
	require.NoError(t, err)
	require.Equal(t, "/app/arcane", path)
	require.Nil(t, executableMount)

	path, executableMount, err = recoveryHelperExecutableInternal([]containertypes.MountPoint{{
		Type: mounttypes.TypeBind, Source: "/workspace/backend", Destination: "/app/backend", RW: true,
	}}, "/app/backend/.bin/arcane")
	require.NoError(t, err)
	require.Equal(t, systemRecoveryHelperPath, path)
	require.Equal(t, mounttypes.TypeBind, executableMount.Type)
	require.Equal(t, "/workspace/backend/.bin/arcane", executableMount.Source)
	require.Equal(t, systemRecoveryHelperPath, executableMount.Target)
	require.True(t, executableMount.ReadOnly)
}

func TestSystemBackupPoliciesRegisterIndependentJobs(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-schedules?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.SystemBackupPolicy{}, &models.SystemBackupRun{}, &models.SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "system-backup-policy-test-key-32bytes", Environment: "test"})
	service := &SystemBackupService{db: &database.DB{DB: gormDB}}
	scheduler := &volumeBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	service.SetScheduler(context.Background(), scheduler)

	status, err := service.SetRecoveryKey(context.Background(), "correct horse battery staple")
	require.NoError(t, err)
	require.True(t, status.Configured)
	storedKey, err := service.recoveryKeyInternal(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "correct horse battery staple", storedKey)

	collection, err := service.UpdatePolicies(context.Background(), []backuptypes.UpdateSystemBackupPolicy{
		{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 5, LocalEnabled: true},
		{Enabled: true, Schedule: "0 0 14 * * *", RetentionCount: 30, LocalEnabled: true},
	})
	require.NoError(t, err)
	require.True(t, collection.RecoveryKeyStored)
	require.Len(t, collection.Policies, 2)
	require.Len(t, scheduler.jobs, 2)

	firstID := collection.Policies[0].ID
	collection, err = service.UpdatePolicies(context.Background(), []backuptypes.UpdateSystemBackupPolicy{{
		ID: firstID, Enabled: true, Schedule: "0 */30 * * * *", RetentionCount: 9, LocalEnabled: true,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.Equal(t, firstID, collection.Policies[0].ID)
	require.Equal(t, 9, collection.Policies[0].RetentionCount)
	require.Len(t, scheduler.jobs, 1)
}

func TestSystemBackupPolicyRequiresConfiguredRecoveryKeyWhenEnabled(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-key-required?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.SystemBackupPolicy{}, &models.SystemBackupRecoveryConfig{}))
	service := &SystemBackupService{db: &database.DB{DB: gormDB}}

	_, err = service.UpdatePolicies(context.Background(), []backuptypes.UpdateSystemBackupPolicy{{
		Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 7, LocalEnabled: true,
	}})
	require.ErrorContains(t, err, "configure a recovery key")
}

func TestSystemBackupPolicyRetentionIgnoresFailedRuns(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-retention-failed?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.SystemBackupRun{}))

	policyID := "policy-1"
	require.NoError(t, gormDB.Create(&models.SystemBackupRun{
		PolicyID: policyID, Status: models.VolumeBackupStatusSucceeded,
		LocalSnapshotID: "snapshot-1", CreatedAt: time.Now().Add(-time.Hour),
	}).Error)
	require.NoError(t, gormDB.Create(&models.SystemBackupRun{
		PolicyID: policyID, Status: models.VolumeBackupStatusFailed,
		CreatedAt: time.Now(),
	}).Error)

	service := &SystemBackupService{db: &database.DB{DB: gormDB}}
	require.NoError(t, service.applyRetentionInternal(context.Background(), policyID, 1))

	var backups []models.SystemBackupRun
	require.NoError(t, gormDB.Order("created_at ASC").Find(&backups).Error)
	require.Len(t, backups, 2)
	require.Equal(t, "snapshot-1", backups[0].LocalSnapshotID)
	require.Equal(t, models.VolumeBackupStatusFailed, backups[1].Status)
}

func TestRecoveryEnvironmentInternalIncludesRuntimeSecrets(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:         "jwt-secret",
		EncryptionKey:     "encryption-secret",
		AdminStaticAPIKey: "admin-key",
		OidcClientSecret:  "oidc-secret",
		FilePerm:          0o640,
	}
	environment := (&SystemBackupService{config: cfg}).recoveryEnvironmentInternal()
	require.Equal(t, "jwt-secret", environment["JWT_SECRET"])
	require.Equal(t, "encryption-secret", environment["ENCRYPTION_KEY"])
	require.Equal(t, "admin-key", environment["ADMIN_STATIC_API_KEY"])
	require.Equal(t, "oidc-secret", environment["OIDC_CLIENT_SECRET"])
	require.Equal(t, "0640", environment["FILE_PERM"])
}

func TestDecodeDiscoveredSnapshotsInternal(t *testing.T) {
	plain, err := decodeDiscoveredSnapshotsInternal(`[{"id":"plain","time":"2026-07-20T10:00:00Z","summary":{"total_bytes_processed":12}}]`)
	require.NoError(t, err)
	require.Equal(t, "plain", plain[0].ID)
	require.EqualValues(t, 12, plain[0].Summary.TotalBytesProcessed)

	grouped, err := decodeDiscoveredSnapshotsInternal(`[{"group_key":{"hostname":"arcane"},"snapshots":[{"id":"grouped","time":"2026-07-20T10:00:00Z"}]}]`)
	require.NoError(t, err)
	require.Equal(t, "grouped", grouped[0].ID)
}

func TestSystemSnapshotPathInternal(t *testing.T) {
	path, err := systemSnapshotPathInternal(`["/arcane.db","/.arcane-recovery.json"]`)
	require.NoError(t, err)
	require.Equal(t, "/", path)

	path, err = systemSnapshotPathInternal(`["/app/data/arcane.db","/app/data/.arcane-recovery.json"]`)
	require.NoError(t, err)
	require.Equal(t, "/app/data", path)

	_, err = systemSnapshotPathInternal(`["/arcane.db"]`)
	require.ErrorContains(t, err, "does not contain an Arcane recovery manifest")
}
