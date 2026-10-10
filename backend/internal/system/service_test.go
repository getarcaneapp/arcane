package system

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	systembackup "github.com/getarcaneapp/arcane/backend/v2/internal/system/children/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
)

func TestResolveSystemUpgraderRuntimeOptionsInternal_TCPDockerHost(t *testing.T) {
	currentContainer := &container.InspectResponse{
		HostConfig: &container.HostConfig{NetworkMode: "bridge"},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge":      {},
				"arcane-test": {},
			},
		},
	}

	containerEnv, mounts, networkMode, err := resolveUpgraderRuntimeOptionsInternal(
		t.Context(),
		"tcp://docker-socket-proxy:2375",
		currentContainer,
		nil,
		func() bool { return true },
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"DOCKER_HOST=tcp://docker-socket-proxy:2375"}, containerEnv)
	require.Empty(t, mounts)
	require.Equal(t, container.NetworkMode("arcane-test"), networkMode)
}

func TestResolveSystemUpgraderRuntimeOptionsInternal_UnixDockerHost(t *testing.T) {
	containerEnv, mounts, networkMode, err := resolveUpgraderRuntimeOptionsInternal(
		t.Context(),
		"unix:///var/run/docker.sock",
		nil,
		func(context.Context, string) (string, error) {
			return "/host/run/docker.sock", nil
		},
		func() bool { return true },
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"DOCKER_HOST=unix:///var/run/docker.sock"}, containerEnv)
	require.Equal(t, container.NetworkMode(""), networkMode)
	require.Equal(t, []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: "/host/run/docker.sock",
			Target: "/var/run/docker.sock",
		},
	}, mounts)
}

func TestResolveSystemUpgraderRuntimeOptionsInternal_DefaultDockerHost(t *testing.T) {
	containerEnv, mounts, _, err := resolveUpgraderRuntimeOptionsInternal(
		t.Context(),
		"",
		nil,
		func(context.Context, string) (string, error) {
			return "/var/run/docker.sock", nil
		},
		func() bool { return true },
		nil,
	)
	require.NoError(t, err)
	require.Nil(t, containerEnv)
	require.Equal(t, []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: "/var/run/docker.sock",
			Target: "/var/run/docker.sock",
		},
	}, mounts)
}

func TestResolveSystemUpgraderRuntimeOptionsInternal_UnixDockerHostResolutionError(t *testing.T) {
	_, _, _, err := resolveUpgraderRuntimeOptionsInternal(
		t.Context(),
		"unix:///var/run/docker.sock",
		nil,
		func(context.Context, string) (string, error) {
			return "", errors.New("inspect failed")
		},
		func() bool { return true },
		nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve unix socket source")
}

func TestListBackupHistoryClassifiesAndFiltersOrigins(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-volume-history?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupRun{}, &volume.VolumeBackup{}))
	now := time.Now().UTC()
	require.NoError(t, gormDB.Create(&SystemBackupRun{
		CreatedAt: now.Add(-2 * time.Minute), Status: SystemBackupStatusSucceeded, Trigger: SystemBackupTriggerManual,
		Destination: backuptypes.SystemBackupDestinationLocal,
	}).Error)
	require.NoError(t, gormDB.Create(&volume.VolumeBackup{
		VolumeName: "app-data", CreatedAt: now.Add(-time.Minute), Status: volume.VolumeBackupStatusSucceeded,
		Trigger: volume.VolumeBackupTriggerScheduled, Destination: volumetypes.BackupDestinationLocal,
		Format: volume.VolumeBackupFormatRustic, PolicyID: backuptypes.SystemVolumePolicyPrefix + "nightly:" + kit.SHA256Hex("app-data")[:16],
	}).Error)
	require.NoError(t, gormDB.Create(&volume.VolumeBackup{
		VolumeName: "cache", CreatedAt: now, Status: volume.VolumeBackupStatusSucceeded,
		Trigger: volume.VolumeBackupTriggerManual, Destination: volumetypes.BackupDestinationLocal,
		Format: volume.VolumeBackupFormatRustic,
	}).Error)
	service := &SystemService{db: &database.DB{DB: gormDB}}
	service.backup = systembackup.NewService(systembackup.Dependencies{Store: service.backupStoreInternal(nil)})

	systemRows, page, err := service.backup.ListBackupHistory(t.Context(), pagination.QueryParams{
		Sort: "createdAt", Order: pagination.SortDesc, Limit: 20,
		Filters: map[string]string{"type": "system"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, systemRows, 2)
	require.Equal(t, backuptypes.ManagementTypeSystem, systemRows[0].Type)
	require.Equal(t, "app-data", systemRows[0].ResourceName)
	require.Equal(t, "volume", systemRows[0].ResourceType)

	volumeRows, page, err := service.backup.ListBackupHistory(t.Context(), pagination.QueryParams{
		Search: "cache", Sort: "createdAt", Order: pagination.SortDesc, Limit: 1,
		Filters: map[string]string{"type": "volume"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, volumeRows, 1)
	require.Equal(t, backuptypes.ManagementTypeVolume, volumeRows[0].Type)
	require.Equal(t, "cache", volumeRows[0].ResourceName)
}

func newSystemBackupAdmissionGateForTestInternal(t testing.TB) *runs.Admission {
	t.Helper()
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	return gate
}

type systemBackupPolicySchedulerInternal struct {
	submitted []scheduler.Request
	jobs      map[string]scheduler.Job
}

func (s *systemBackupPolicySchedulerInternal) AddJob(_ context.Context, job scheduler.Job) error {
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

func (s *systemBackupPolicySchedulerInternal) Submit(_ context.Context, request scheduler.Request) (scheduler.Run, error) {
	s.submitted = append(s.submitted, request)
	return scheduler.Run{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: scheduler.Queued}, nil
}

func TestSystemBackupPoliciesRegisterIndependentJobs(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-schedules?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &SystemBackupRun{}, &backup.SystemBackupRecoveryConfig{}, &s3.S3Destination{}, &activity.Activity{}, &activity.ActivityMessage{}))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "system-backup-policy-test-key-32bytes", Environment: "test"})
	db := &database.DB{DB: gormDB}
	recoveryKeys := backup.NewRecoveryKeyStore(db)
	s3Destinations := s3.NewS3DestinationService(db, nil)
	service := &SystemService{db: db}
	service.backup = systembackup.NewService(systembackup.Dependencies{
		Store:          service.backupStoreInternal(s3Destinations),
		DB:             db,
		SQLDB:          db.SQLDB,
		S3Destinations: s3Destinations,
		Config:         &config.Config{DatabaseURL: "file:system-backup-schedules-test.db"},
		RecoveryKeys:   recoveryKeys,
	})
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	require.NoError(t, service.backup.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	jobScheduler := &systemBackupPolicySchedulerInternal{jobs: make(map[string]scheduler.Job)}
	require.NoError(t, service.SetBackupScheduler(t.Context(), jobScheduler, newSystemBackupAdmissionGateForTestInternal(t)))

	status, err := service.backup.SetRecoveryKey(t.Context(), "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN")
	require.NoError(t, err)
	require.True(t, status.Configured)
	storedKey, err := recoveryKeys.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN", storedKey)

	collection, err := service.backup.UpdatePolicies(t.Context(), []backuptypes.UpdateSystemBackupPolicy{
		{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 5, LocalEnabled: true},
		{Enabled: true, Schedule: "0 0 14 * * *", RetentionCount: 30, LocalEnabled: true},
	})
	require.NoError(t, err)
	require.True(t, collection.RecoveryKeyStored)
	require.Len(t, collection.Policies, 2)
	require.Len(t, jobScheduler.jobs, 2)

	firstID := collection.Policies[0].ID
	collection, err = service.backup.UpdatePolicies(t.Context(), []backuptypes.UpdateSystemBackupPolicy{{
		ID: firstID, Enabled: true, Schedule: "0 */30 * * * *", RetentionCount: 9, LocalEnabled: true,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.Equal(t, firstID, collection.Policies[0].ID)
	require.Equal(t, 9, collection.Policies[0].RetentionCount)
	require.Len(t, jobScheduler.jobs, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<Error><Code>NoSuchBucket</Code></Error>"))
	}))
	defer server.Close()
	destination, err := s3Destinations.CreateS3Destination(t.Context(), backuptypes.CreateS3Destination{
		Name: "Missing storage", Endpoint: server.URL, Bucket: "backups", AccessKeyID: "test", SecretAccessKey: "test", ForcePathStyle: true,
	})
	require.NoError(t, err)
	for _, local := range []bool{false, true} {
		policy := &SystemBackupPolicy{Enabled: true, LocalEnabled: local, S3Enabled: true, S3DestinationID: destination.ID, Schedule: "0 0 2 * * *"}
		require.NoError(t, gormDB.Create(policy).Error)
		require.NoError(t, gormDB.Model(policy).Update("local_enabled", local).Error)
		service.RegisterBackupJobOnStartup(t.Context())
		jobName := "system-backup:" + policy.ID
		job, registered := jobScheduler.jobs[jobName]
		require.True(t, registered)
		// The scheduled run checks the remote first; a local policy then
		// attempts its backup, which has no engine in this test.
		outcome, runErr := job.Run(t.Context())
		require.NoError(t, runErr)
		if local {
			require.Equal(t, scheduler.Failed, outcome.Status)
			require.Contains(t, outcome.Message, "backup engine is unavailable")
		} else {
			require.Equal(t, scheduler.NeedsAttention, outcome.Status)
			require.Equal(t, backup.RemoteDisabledMessage, outcome.Message)
		}
		require.NoError(t, gormDB.First(policy, "id = ?", policy.ID).Error)
		require.Equal(t, local, policy.Enabled)
		require.Equal(t, !local, policy.S3Enabled)
		require.Equal(t, local, jobScheduler.HasJob(jobName))
	}
}

func TestSystemBackupPolicyRequiresConfiguredRecoveryKeyWhenEnabled(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-key-required?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &backup.SystemBackupRecoveryConfig{}))
	db := &database.DB{DB: gormDB}
	service := &SystemService{db: db}
	service.backup = systembackup.NewService(systembackup.Dependencies{
		Store:        service.backupStoreInternal(nil),
		DB:           db,
		Config:       &config.Config{DatabaseURL: "file:system-backup-key-required-test.db"},
		RecoveryKeys: backup.NewRecoveryKeyStore(db),
	})

	_, err = service.backup.UpdatePolicies(t.Context(), []backuptypes.UpdateSystemBackupPolicy{{
		Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 7, LocalEnabled: true,
	}})
	require.ErrorContains(t, err, "configure a recovery key")
}
