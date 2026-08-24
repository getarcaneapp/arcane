package systembackup

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func newSystemVolumeSettingsServiceInternal(t *testing.T) *settings.SettingsService {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	writes, err := actors.NewExecutor(t.Context(), runtime, "system-volume-settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "system-volume-settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	service, err := settings.NewSettingsService(t.Context(), db, writes, effects)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, writes.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return service
}

func TestSystemVolumeBackupSelectionInternal(t *testing.T) {
	options := []backuptypes.SystemVolumeBackupOption{
		{Name: "app", Available: true},
		{Name: "cache", Available: true},
		{Name: "anonymous", Anonymous: true, Available: true},
		{Name: "deleted", Available: false},
	}
	tests := []struct {
		name     string
		config   backuptypes.SystemVolumeBackupConfig
		expected []string
	}{
		{
			name:     "all includes future named volumes",
			config:   backuptypes.SystemVolumeBackupConfig{SelectionMode: backuptypes.SystemVolumeSelectionAll, IgnoreAnonymous: true},
			expected: []string{"app", "cache"},
		},
		{
			name:     "allowlist includes exact live names only",
			config:   backuptypes.SystemVolumeBackupConfig{SelectionMode: backuptypes.SystemVolumeSelectionAllowlist, VolumeNames: []string{"app", "deleted"}},
			expected: []string{"app"},
		},
		{
			name:     "blocklist includes unselected future and anonymous volumes",
			config:   backuptypes.SystemVolumeBackupConfig{SelectionMode: backuptypes.SystemVolumeSelectionBlocklist, VolumeNames: []string{"cache"}},
			expected: []string{"app", "anonymous"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected := selectSystemVolumeBackupCandidatesInternal(tt.config, options)
			names := make([]string, len(selected))
			for i := range selected {
				names[i] = selected[i].Name
			}
			require.Equal(t, tt.expected, names)
		})
	}
}

func TestNormalizeVolumeNamesInternal(t *testing.T) {
	require.Equal(t, []string{"app", "cache"}, normalizeVolumeNamesInternal([]string{" cache ", "app", "cache", ""}))
}

func TestSystemVolumePolicyIDInternalIsStableAndScoped(t *testing.T) {
	first := systemVolumePolicyIDInternal("app-data")
	require.Equal(t, first, systemVolumePolicyIDInternal("app-data"))
	require.NotEqual(t, first, systemVolumePolicyIDInternal("other-data"))
	require.Contains(t, first, backuptypes.SystemVolumePolicyPrefix)
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
		Format: volume.VolumeBackupFormatRustic, PolicyID: systemVolumePolicyIDInternal("app-data"),
	}).Error)
	require.NoError(t, gormDB.Create(&volume.VolumeBackup{
		VolumeName: "cache", CreatedAt: now, Status: volume.VolumeBackupStatusSucceeded,
		Trigger: volume.VolumeBackupTriggerManual, Destination: volumetypes.BackupDestinationLocal,
		Format: volume.VolumeBackupFormatRustic,
	}).Error)
	service := &SystemBackupService{db: &database.DB{DB: gormDB}}

	systemRows, page, err := service.ListBackupHistory(context.Background(), pagination.QueryParams{
		Sort: "createdAt", Order: pagination.SortDesc, Limit: 20,
	}, "system")
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, systemRows, 2)
	require.Equal(t, backuptypes.ManagementTypeSystem, systemRows[0].Type)
	require.Equal(t, "app-data", systemRows[0].ResourceName)
	require.Equal(t, "volume", systemRows[0].ResourceType)

	volumeRows, page, err := service.ListBackupHistory(context.Background(), pagination.QueryParams{
		Search: "cache", Sort: "createdAt", Order: pagination.SortDesc, Limit: 1,
	}, "volume")
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, volumeRows, 1)
	require.Equal(t, backuptypes.ManagementTypeVolume, volumeRows[0].Type)
	require.Equal(t, "cache", volumeRows[0].ResourceName)
}

func TestDefaultSystemVolumeBackupConfigInternal(t *testing.T) {
	config := defaultSystemVolumeBackupConfigInternal()
	require.False(t, config.Enabled)
	require.Equal(t, defaultSystemVolumeSchedule, config.Schedule)
	require.Equal(t, 7, config.RetentionCount)
	require.True(t, config.LocalEnabled)
	require.True(t, config.IgnoreAnonymous)
	require.Equal(t, backuptypes.SystemVolumeSelectionAll, config.SelectionMode)
}

func TestSystemVolumeBackupConfigPersistsAndReschedulesInternal(t *testing.T) {
	settingsService := newSystemVolumeSettingsServiceInternal(t)
	service := &SystemBackupService{
		settingsService: settingsService,
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	scheduler := &systemBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	require.NoError(t, service.SetScheduler(context.Background(), scheduler, newSystemBackupAdmissionGateForTestInternal(t)))

	saved, err := service.UpdateSystemVolumeBackupConfig(context.Background(), backuptypes.SystemVolumeBackupConfig{
		Enabled: true, Schedule: "0 15 2 * * *", RetentionCount: 9, LocalEnabled: true,
		SelectionMode:   backuptypes.SystemVolumeSelectionBlocklist,
		VolumeNames:     []string{" cache ", "app", "cache", ""},
		IgnoreAnonymous: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"app", "cache"}, saved.VolumeNames)
	jobName := service.jobs.JobName(systemVolumeBackupJobID)
	require.True(t, scheduler.HasJob(jobName))
	require.Equal(t, saved.Schedule, scheduler.jobs[jobName].Schedule(context.Background()))

	reloaded, err := service.GetSystemVolumeBackupConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, saved, reloaded)

	saved.Enabled = false
	_, err = service.UpdateSystemVolumeBackupConfig(context.Background(), *saved)
	require.NoError(t, err)
	require.False(t, scheduler.HasJob(jobName))
}

func TestSystemVolumeBackupConfigValidationInternal(t *testing.T) {
	service := &SystemBackupService{
		settingsService: newSystemVolumeSettingsServiceInternal(t),
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	valid := defaultSystemVolumeBackupConfigInternal()
	tests := []struct {
		name   string
		mutate func(*backuptypes.SystemVolumeBackupConfig)
	}{
		{name: "selection mode", mutate: func(config *backuptypes.SystemVolumeBackupConfig) { config.SelectionMode = "snapshot" }},
		{name: "cron", mutate: func(config *backuptypes.SystemVolumeBackupConfig) { config.Schedule = "not cron" }},
		{name: "retention", mutate: func(config *backuptypes.SystemVolumeBackupConfig) { config.RetentionCount = 3651 }},
		{name: "destination", mutate: func(config *backuptypes.SystemVolumeBackupConfig) { config.LocalEnabled = false }},
		{name: "s3 destination", mutate: func(config *backuptypes.SystemVolumeBackupConfig) {
			config.LocalEnabled = false
			config.S3Enabled = true
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.mutate(&config)
			_, err := service.UpdateSystemVolumeBackupConfig(context.Background(), config)
			require.Error(t, err)
		})
	}
}

func TestSystemVolumeBackupAllModeDoesNotPersistNamesInternal(t *testing.T) {
	service := &SystemBackupService{
		settingsService: newSystemVolumeSettingsServiceInternal(t),
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	config := defaultSystemVolumeBackupConfigInternal()
	config.VolumeNames = []string{"old-selection"}

	saved, err := service.UpdateSystemVolumeBackupConfig(context.Background(), config)
	require.NoError(t, err)
	require.Empty(t, saved.VolumeNames)
}
