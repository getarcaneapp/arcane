package system

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis/testing"
)

func TestResolveSystemUpgraderRuntimeOptions_TCPDockerHost(t *testing.T) {
	currentContainer := &container.InspectResponse{
		HostConfig: &container.HostConfig{NetworkMode: "bridge"},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge":      {},
				"arcane-test": {},
			},
		},
	}

	containerEnv, mounts, networkMode, err := resolveUpgraderRuntimeOptions(
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

func TestResolveSystemUpgraderRuntimeOptions_UnixDockerHost(t *testing.T) {
	containerEnv, mounts, networkMode, err := resolveUpgraderRuntimeOptions(
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

func TestResolveSystemUpgraderRuntimeOptions_DefaultDockerHost(t *testing.T) {
	containerEnv, mounts, _, err := resolveUpgraderRuntimeOptions(
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

func TestResolveSystemUpgraderRuntimeOptions_UnixDockerHostResolutionError(t *testing.T) {
	_, _, _, err := resolveUpgraderRuntimeOptions(
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
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupRun{}, &volume.VolumeBackup{}, &s3.S3Destination{}, &settings.SettingVariable{}))
	now := time.Now().UTC()
	require.NoError(t, gormDB.Create(&SystemBackupRun{
		CreatedAt: now.Add(-2 * time.Minute), Status: SystemBackupStatusSucceeded, Trigger: SystemBackupTriggerManual,
		Destination: types.SystemBackupDestinationLocal,
	}).Error)
	require.NoError(t, gormDB.Create(&volume.VolumeBackup{
		VolumeName: "app-data", CreatedAt: now.Add(-time.Minute), Status: volume.VolumeBackupStatusSucceeded,
		Trigger: volume.VolumeBackupTriggerScheduled, Destination: types.VolumeBackupDestinationLocal,
		Format: volume.VolumeBackupFormatRustic, PolicyID: types.SystemVolumePolicyPrefix + "nightly:" + kit.SHA256Hex("app-data")[:16],
	}).Error)
	require.NoError(t, gormDB.Create(&volume.VolumeBackup{
		VolumeName: "cache", CreatedAt: now, Status: volume.VolumeBackupStatusSucceeded,
		Trigger: volume.VolumeBackupTriggerManual, Destination: types.VolumeBackupDestinationLocal,
		Format: volume.VolumeBackupFormatRustic,
	}).Error)
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(t.Context(), db, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	service := NewSystemService(
		db, nil, nil, nil, nil, nil, settingsService, nil, nil, nil, nil, nil, nil,
		s3.NewS3DestinationService(db, func(context.Context, string) error { return nil }), nil, nil, &config.Config{},
		nil,
	)

	systemRows, page, err := service.backup.ListBackupHistory(t.Context(), pagination.QueryParams{
		Sort: "createdAt", Order: pagination.SortDesc, Limit: 20,
		Filters: map[string]string{"type": "system"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, systemRows, 2)
	require.Equal(t, types.BackupManagementTypeSystem, systemRows[0].Type)
	require.Equal(t, "app-data", systemRows[0].ResourceName)
	require.Equal(t, "volume", systemRows[0].ResourceType)

	volumeRows, page, err := service.backup.ListBackupHistory(t.Context(), pagination.QueryParams{
		Search: "cache", Sort: "createdAt", Order: pagination.SortDesc, Limit: 1,
		Filters: map[string]string{"type": "volume"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, volumeRows, 1)
	require.Equal(t, types.BackupManagementTypeVolume, volumeRows[0].Type)
	require.Equal(t, "cache", volumeRows[0].ResourceName)
}

func newSystemBackupAdmissionGateForTest(t testing.TB) *runs.Admission {
	t.Helper()
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	return gate
}

type systemBackupPolicyScheduler struct {
	submitted []types.SchedulerRequest
	jobs      map[string]types.SchedulerJob
}

func (s *systemBackupPolicyScheduler) AddJob(_ context.Context, job types.SchedulerJob) error {
	s.jobs[job.Name()] = job
	return nil
}

func (s *systemBackupPolicyScheduler) RemoveJob(_ context.Context, name string) {
	delete(s.jobs, name)
}

func (s *systemBackupPolicyScheduler) HasJob(name string) bool {
	_, ok := s.jobs[name]
	return ok
}

func (s *systemBackupPolicyScheduler) Submit(_ context.Context, request types.SchedulerRequest) (types.SchedulerRun, error) {
	s.submitted = append(s.submitted, request)
	return types.SchedulerRun{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: types.RunStatusQueued}, nil
}

func TestSystemBackupPoliciesRegisterIndependentJobs(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-schedules?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(
		&SystemBackupPolicy{},
		&SystemBackupRun{},
		&backup.SystemBackupRecoveryConfig{},
		&s3.S3Destination{},
		&activity.Activity{},
		&activity.ActivityMessage{},
		&settings.SettingVariable{},
		&volume.VolumeBackup{},
	))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "system-backup-policy-test-key-32bytes", Environment: "test"})
	db := &database.DB{DB: gormDB}
	recoveryKeys := backup.NewRecoveryKeyStore(db)
	s3Destinations := s3.NewS3DestinationService(db, func(context.Context, string) error { return nil })
	settingsService, err := settings.NewSettingsService(t.Context(), db, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	gate := newSystemBackupAdmissionGateForTest(t)
	dockerService := (&docker.DockerClientService{}).WithClient(func() *client.Client {
		cli, _ := client.New(client.WithHost("tcp://127.0.0.1:1"), client.WithAPIVersion("1.41"))
		return cli
	}())
	service := NewSystemService(
		db,
		dockerService,
		nil,
		nil,
		volume.NewVolumeService(db, dockerService, nil, nil, nil, nil, nil, nil, nil, nil),
		nil,
		settingsService,
		nil,
		nil,
		nil,
		nil,
		nil,
		backup.NewEngine(gate, nil),
		s3Destinations,
		recoveryKeys,
		nil,
		&config.Config{DatabaseURL: "file:system-backup-schedules-test.db"},
		nil,
	)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	require.NoError(t, service.backup.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	jobScheduler := &systemBackupPolicyScheduler{jobs: make(map[string]types.SchedulerJob)}
	require.NoError(t, service.SetBackupScheduler(t.Context(), jobScheduler, gate))

	status, err := service.backup.SetRecoveryKey(t.Context(), "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN")
	require.NoError(t, err)
	require.True(t, status.Configured)
	storedKey, err := recoveryKeys.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN", storedKey)

	collection, err := service.backup.UpdatePolicies(t.Context(), []types.UpdateBackupPolicy{
		{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 5, LocalEnabled: true},
		{Enabled: true, Schedule: "0 0 14 * * *", RetentionCount: 30, LocalEnabled: true},
	})
	require.NoError(t, err)
	require.True(t, collection.RecoveryKeyStored)
	require.Len(t, collection.Policies, 2)
	require.Len(t, jobScheduler.jobs, 2)

	firstID := collection.Policies[0].ID
	collection, err = service.backup.UpdatePolicies(t.Context(), []types.UpdateBackupPolicy{{
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
	destination, err := s3Destinations.CreateS3Destination(t.Context(), types.S3Destination{
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
		// attempts its backup, which has no reachable Docker in this test.
		outcome, runErr := job.Run(t.Context())
		require.NoError(t, runErr)
		if local {
			require.Equal(t, types.RunStatusFailed, outcome.Status)
			require.Contains(t, outcome.Message, "failed to create local system recovery snapshot")
		} else {
			require.Equal(t, types.RunStatusNeedsAttention, outcome.Status)
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
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &backup.SystemBackupRecoveryConfig{}, &s3.S3Destination{}))
	db := &database.DB{DB: gormDB}
	service := NewSystemService(
		db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		s3.NewS3DestinationService(db, func(context.Context, string) error { return nil }),
		backup.NewRecoveryKeyStore(db), nil, &config.Config{DatabaseURL: "file:system-backup-key-required-test.db"},
		nil,
	)

	_, err = service.backup.UpdatePolicies(t.Context(), []types.UpdateBackupPolicy{{
		Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 7, LocalEnabled: true,
	}})
	require.ErrorContains(t, err, "configure a recovery key")
}
