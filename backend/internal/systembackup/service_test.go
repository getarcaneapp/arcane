package systembackup

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	sqlite "github.com/libtnb/sqlite"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func newSystemBackupAdmissionGateForTestInternal(t testing.TB) *actors.Gate[actors.AdmissionKey] {
	t.Helper()
	fxLifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), fxLifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), runtime, "system-backup-test-admission", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, gate.Stop(stopCtx))
		require.NoError(t, fxLifecycle.Stop(stopCtx))
	})
	return gate
}

type systemBackupPolicySchedulerInternal struct {
	jobs map[string]schedulertypes.Job
}

func (s *systemBackupPolicySchedulerInternal) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.jobs[job.Name()] = job
	return nil
}

func (s *systemBackupPolicySchedulerInternal) RemoveJob(_ context.Context, name string) {
	delete(s.jobs, name)
}

func (s *systemBackupPolicySchedulerInternal) HasJob(name string) bool {
	_, ok := s.jobs[name]
	return ok
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
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &SystemBackupRun{}, &SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "system-backup-policy-test-key-32bytes", Environment: "test"})
	service := &SystemBackupService{
		db:     &database.DB{DB: gormDB},
		config: &config.Config{DatabaseURL: "file:system-backup-schedules-test.db"},
		jobs:   entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	scheduler := &systemBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	require.NoError(t, service.SetScheduler(context.Background(), scheduler, newSystemBackupAdmissionGateForTestInternal(t)))

	status, err := service.SetRecoveryKey(context.Background(), "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN")
	require.NoError(t, err)
	require.True(t, status.Configured)
	storedKey, err := service.recoveryKeyInternal(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN", storedKey)

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
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &SystemBackupRecoveryConfig{}))
	service := &SystemBackupService{
		db:     &database.DB{DB: gormDB},
		config: &config.Config{DatabaseURL: "file:system-backup-key-required-test.db"},
	}

	_, err = service.UpdatePolicies(context.Background(), []backuptypes.UpdateSystemBackupPolicy{{
		Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 7, LocalEnabled: true,
	}})
	require.ErrorContains(t, err, "configure a recovery key")
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

func TestSystemSnapshotPathFromFilesInternal(t *testing.T) {
	path, err := systemSnapshotPathFromFilesInternal([]string{"/arcane.db", "/.arcane-recovery.json"})
	require.NoError(t, err)
	require.Equal(t, "/", path)

	path, err = systemSnapshotPathFromFilesInternal([]string{"/app/data/arcane.db", "/app/data/.arcane-recovery.json"})
	require.NoError(t, err)
	require.Equal(t, "/app/data", path)

	_, err = systemSnapshotPathFromFilesInternal([]string{"/arcane.db"})
	require.ErrorContains(t, err, "does not contain an Arcane recovery manifest")
}
