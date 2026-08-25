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
		config   backuptypes.SystemVolumeBackupPolicy
		expected []string
	}{
		{
			name:     "all includes future named volumes",
			config:   backuptypes.SystemVolumeBackupPolicy{SelectionMode: backuptypes.SystemVolumeSelectionAll, IgnoreAnonymous: true},
			expected: []string{"app", "cache"},
		},
		{
			name:     "allowlist includes exact live names only",
			config:   backuptypes.SystemVolumeBackupPolicy{SelectionMode: backuptypes.SystemVolumeSelectionAllowlist, VolumeNames: []string{"app", "deleted"}},
			expected: []string{"app"},
		},
		{
			name:     "blocklist includes unselected future and anonymous volumes",
			config:   backuptypes.SystemVolumeBackupPolicy{SelectionMode: backuptypes.SystemVolumeSelectionBlocklist, VolumeNames: []string{"cache"}},
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

func TestSystemVolumePolicyIDInternalIsStableAndScoped(t *testing.T) {
	first := systemVolumePolicyIDInternal("nightly", "app-data")
	require.Equal(t, first, systemVolumePolicyIDInternal("nightly", "app-data"))
	require.NotEqual(t, first, systemVolumePolicyIDInternal("daily", "app-data"))
	require.NotEqual(t, first, systemVolumePolicyIDInternal("nightly", "other-data"))
	require.Contains(t, first, backuptypes.SystemVolumePolicyPrefix)
	require.NotEqual(t, first, systemVolumeManualPolicyIDInternal("app-data"))
	require.NotContains(t, systemVolumePolicyIDInternal(legacySystemVolumePolicyID, "app-data")[len(backuptypes.SystemVolumePolicyPrefix):], ":")
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
		Format: volume.VolumeBackupFormatRustic, PolicyID: systemVolumePolicyIDInternal("nightly", "app-data"),
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

func TestListBackupHistoryUsesStablePaginatedOrdering(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-volume-history-order?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupRun{}, &volume.VolumeBackup{}))
	createdAt := time.Now().UTC()
	for _, id := range []string{"backup-b", "backup-a"} {
		require.NoError(t, gormDB.Create(&SystemBackupRun{
			BaseModel:   database.BaseModel{ID: id},
			CreatedAt:   createdAt,
			Status:      SystemBackupStatusSucceeded,
			Trigger:     SystemBackupTriggerManual,
			Destination: backuptypes.SystemBackupDestinationLocal,
		}).Error)
	}
	service := &SystemBackupService{db: &database.DB{DB: gormDB}}

	first, firstPage, err := service.ListBackupHistory(context.Background(), pagination.QueryParams{
		Sort: "createdAt", Order: pagination.SortDesc, Limit: 1,
	}, "")
	require.NoError(t, err)
	second, secondPage, err := service.ListBackupHistory(context.Background(), pagination.QueryParams{
		Sort: "createdAt", Order: pagination.SortDesc, Start: 1, Limit: 1,
	}, "")
	require.NoError(t, err)
	require.EqualValues(t, 2, firstPage.TotalItems)
	require.Equal(t, 1, firstPage.ItemsPerPage)
	require.Equal(t, 2, secondPage.CurrentPage)
	require.Equal(t, "backup-a", first[0].ID)
	require.Equal(t, "backup-b", second[0].ID)
}

func TestDefaultSystemVolumeBackupPolicyInternal(t *testing.T) {
	config := defaultSystemVolumeBackupPolicyInternal()
	require.False(t, config.Enabled)
	require.Equal(t, defaultSystemVolumeSchedule, config.Schedule)
	require.Equal(t, 7, config.RetentionCount)
	require.True(t, config.LocalEnabled)
	require.True(t, config.IgnoreAnonymous)
	require.Equal(t, backuptypes.SystemVolumeSelectionAll, config.SelectionMode)
}

func TestSystemVolumeBackupPolicyCollectionDefaultsEmptyInternal(t *testing.T) {
	service := &SystemBackupService{settingsService: newSystemVolumeSettingsServiceInternal(t)}

	loaded, err := service.GetSystemVolumeBackupConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loaded.Policies)
	require.Empty(t, loaded.Policies)
}

func defaultSystemVolumePolicyUpdateForTestInternal() backuptypes.UpdateSystemVolumeBackupPolicy {
	policy := defaultSystemVolumeBackupPolicyInternal()
	return backuptypes.UpdateSystemVolumeBackupPolicy{
		Enabled: policy.Enabled, Schedule: policy.Schedule, RetentionCount: policy.RetentionCount,
		StopContainers: policy.StopContainers, LocalEnabled: policy.LocalEnabled, S3Enabled: policy.S3Enabled,
		S3DestinationID: policy.S3DestinationID,
		SelectionMode:   policy.SelectionMode, VolumeNames: policy.VolumeNames, IgnoreAnonymous: policy.IgnoreAnonymous,
	}
}

func TestSystemVolumeBackupPoliciesPersistAndRescheduleIndependentlyInternal(t *testing.T) {
	settingsService := newSystemVolumeSettingsServiceInternal(t)
	service := &SystemBackupService{
		settingsService: settingsService,
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	scheduler := &systemBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	require.NoError(t, service.SetScheduler(context.Background(), scheduler, newSystemBackupAdmissionGateForTestInternal(t)))

	first := defaultSystemVolumePolicyUpdateForTestInternal()
	first.Enabled, first.Schedule, first.RetentionCount = true, "0 15 2 * * *", 9
	first.SelectionMode = backuptypes.SystemVolumeSelectionBlocklist
	first.VolumeNames = []string{" cache ", "app", "cache", ""}
	second := defaultSystemVolumePolicyUpdateForTestInternal()
	second.Enabled, second.Schedule = true, "0 30 4 * * *"
	saved, err := service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{first, second})
	require.NoError(t, err)
	require.Len(t, saved.Policies, 2)
	require.Equal(t, []string{"app", "cache"}, saved.Policies[0].VolumeNames)
	require.NotEmpty(t, saved.Policies[0].ID)
	require.NotEqual(t, saved.Policies[0].ID, saved.Policies[1].ID)
	firstJobName := service.jobs.JobName(systemVolumeBackupJobPrefix + saved.Policies[0].ID)
	secondJobName := service.jobs.JobName(systemVolumeBackupJobPrefix + saved.Policies[1].ID)
	require.True(t, scheduler.HasJob(firstJobName))
	require.True(t, scheduler.HasJob(secondJobName))
	require.Equal(t, saved.Policies[0].Schedule, scheduler.jobs[firstJobName].Schedule(context.Background()))

	reloaded, err := service.GetSystemVolumeBackupConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, saved, reloaded)

	remaining := defaultSystemVolumePolicyUpdateForTestInternal()
	remaining.ID = saved.Policies[1].ID
	remaining.Enabled = false
	_, err = service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{remaining})
	require.NoError(t, err)
	require.False(t, scheduler.HasJob(firstJobName))
	require.False(t, scheduler.HasJob(secondJobName))
}

func TestSystemVolumeBackupPolicyReconciliationRejectsInvalidIDsInternal(t *testing.T) {
	service := &SystemBackupService{
		settingsService: newSystemVolumeSettingsServiceInternal(t),
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	update := defaultSystemVolumePolicyUpdateForTestInternal()
	saved, err := service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{update})
	require.NoError(t, err)
	require.Len(t, saved.Policies, 1)

	existing := update
	existing.ID = saved.Policies[0].ID
	_, err = service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{existing, existing})
	require.ErrorContains(t, err, "duplicate system-managed volume backup policy")

	missing := update
	missing.ID = "missing"
	_, err = service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{missing})
	require.ErrorContains(t, err, "system-managed volume backup policy not found")

	loaded, err := service.GetSystemVolumeBackupConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
}

func TestSystemVolumeBackupPolicyValidationInternal(t *testing.T) {
	service := &SystemBackupService{
		settingsService: newSystemVolumeSettingsServiceInternal(t),
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	valid := defaultSystemVolumePolicyUpdateForTestInternal()
	tests := []struct {
		name   string
		mutate func(*backuptypes.UpdateSystemVolumeBackupPolicy)
	}{
		{name: "selection mode", mutate: func(config *backuptypes.UpdateSystemVolumeBackupPolicy) { config.SelectionMode = "snapshot" }},
		{name: "cron", mutate: func(config *backuptypes.UpdateSystemVolumeBackupPolicy) { config.Schedule = "not cron" }},
		{name: "retention", mutate: func(config *backuptypes.UpdateSystemVolumeBackupPolicy) { config.RetentionCount = 3651 }},
		{name: "destination", mutate: func(config *backuptypes.UpdateSystemVolumeBackupPolicy) { config.LocalEnabled = false }},
		{name: "s3 destination", mutate: func(config *backuptypes.UpdateSystemVolumeBackupPolicy) {
			config.LocalEnabled = false
			config.S3Enabled = true
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.mutate(&config)
			_, err := service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{config})
			require.Error(t, err)
		})
	}
}

func TestSystemVolumeBackupAllModeDoesNotPersistNamesInternal(t *testing.T) {
	service := &SystemBackupService{
		settingsService: newSystemVolumeSettingsServiceInternal(t),
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	config := defaultSystemVolumePolicyUpdateForTestInternal()
	config.VolumeNames = []string{"old-selection"}

	saved, err := service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{config})
	require.NoError(t, err)
	require.Empty(t, saved.Policies[0].VolumeNames)
}

func TestSystemVolumeBackupConfigLoadsLegacySingletonInternal(t *testing.T) {
	settingsService := newSystemVolumeSettingsServiceInternal(t)
	require.NoError(t, settingsService.UpdateSetting(context.Background(), systemVolumeBackupConfigKey, `{"enabled":true,"schedule":"0 0 5 * * *","retentionCount":3,"stopContainers":false,"localEnabled":true,"s3Enabled":false,"selectionMode":"allowlist","volumeNames":["app"],"ignoreAnonymous":true}`))
	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&volume.VolumeBackup{}))
	legacyPolicyID := systemVolumePolicyIDInternal(legacySystemVolumePolicyID, "app")
	require.NoError(t, gormDB.Create(&volume.VolumeBackup{
		VolumeName: "app", CreatedAt: time.Now().Add(-time.Minute), Status: volume.VolumeBackupStatusSucceeded,
		Trigger: volume.VolumeBackupTriggerScheduled, Destination: volumetypes.BackupDestinationLocal,
		Format: volume.VolumeBackupFormatRustic, PolicyID: legacyPolicyID,
	}).Error)
	require.NoError(t, gormDB.Create(&volume.VolumeBackup{
		VolumeName: "other", CreatedAt: time.Now(), Status: volume.VolumeBackupStatusSucceeded,
		Trigger: volume.VolumeBackupTriggerManual, Destination: volumetypes.BackupDestinationLocal,
		Format: volume.VolumeBackupFormatRustic, PolicyID: systemVolumeManualPolicyIDInternal("other"),
	}).Error)
	service := &SystemBackupService{db: &database.DB{DB: gormDB}, settingsService: settingsService, jobs: entityjobs.New("system-backup:", backup.SystemAdmissionScope)}

	loaded, err := service.GetSystemVolumeBackupConfig(context.Background())
	require.NoError(t, err)
	require.Len(t, loaded.Policies, 1)
	require.Equal(t, legacySystemVolumePolicyID, loaded.Policies[0].ID)
	require.Equal(t, []string{"app"}, loaded.Policies[0].VolumeNames)
	require.Equal(t, legacyPolicyID, loaded.Policies[0].LastRun.PolicyID)
}

func TestSystemVolumeBackupConfigNormalizesNullPolicyCollectionInternal(t *testing.T) {
	settingsService := newSystemVolumeSettingsServiceInternal(t)
	require.NoError(t, settingsService.UpdateSetting(context.Background(), systemVolumeBackupConfigKey, `{"policies":null}`))
	service := &SystemBackupService{settingsService: settingsService, jobs: entityjobs.New("system-backup:", backup.SystemAdmissionScope)}

	loaded, err := service.GetSystemVolumeBackupConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loaded.Policies)
	require.Empty(t, loaded.Policies)
}

func TestCustomSystemVolumePolicyDefaultsAndManualIdentityInternal(t *testing.T) {
	service := &SystemBackupService{}
	policy, manual, err := service.resolveSystemVolumeRunPolicyInternal(context.Background(), backuptypes.RunSystemVolumeBackupsRequest{})
	require.NoError(t, err)
	require.True(t, manual)
	require.True(t, policy.LocalEnabled)
	require.Zero(t, policy.RetentionCount)
	require.True(t, policy.IgnoreAnonymous)
	require.Equal(t, backuptypes.SystemVolumeSelectionAll, policy.SelectionMode)
	require.Contains(t, systemVolumeManualPolicyIDInternal("app"), backuptypes.SystemVolumePolicyPrefix+"manual:")
}

func TestResolveSystemVolumeRunPolicySupportsDisabledSavedPolicyInternal(t *testing.T) {
	service := &SystemBackupService{
		settingsService: newSystemVolumeSettingsServiceInternal(t),
		jobs:            entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	update := defaultSystemVolumePolicyUpdateForTestInternal()
	update.Enabled = false
	saved, err := service.UpdateSystemVolumeBackupConfig(context.Background(), []backuptypes.UpdateSystemVolumeBackupPolicy{update})
	require.NoError(t, err)

	policy, manual, err := service.resolveSystemVolumeRunPolicyInternal(context.Background(), backuptypes.RunSystemVolumeBackupsRequest{PolicyID: saved.Policies[0].ID})
	require.NoError(t, err)
	require.False(t, manual)
	require.False(t, policy.Enabled)

	_, _, err = service.resolveSystemVolumeRunPolicyInternal(context.Background(), backuptypes.RunSystemVolumeBackupsRequest{
		PolicyID: saved.Policies[0].ID,
		Custom:   &backuptypes.SystemVolumeBackupCustomRun{Destination: backuptypes.SystemBackupDestinationLocal},
	})
	require.Error(t, err)

	_, _, err = service.resolveSystemVolumeRunPolicyInternal(context.Background(), backuptypes.RunSystemVolumeBackupsRequest{
		Custom: &backuptypes.SystemVolumeBackupCustomRun{Destination: "archive"},
	})
	require.Error(t, err)
}

func TestRunSystemVolumeBackupsRejectsAdmissionContentionInternal(t *testing.T) {
	gate := newSystemBackupAdmissionGateForTestInternal(t)
	engine := backup.NewEngine(t.Context(), nil, gate, nil)
	lease, admitted, err := engine.TryAcquireRun(t.Context(), backup.SystemAdmissionScope, systemAdmissionID)
	require.NoError(t, err)
	require.True(t, admitted)
	t.Cleanup(lease.Release)
	service := &SystemBackupService{volumeService: &volume.VolumeService{}, engine: engine}

	_, err = service.RunSystemVolumeBackups(t.Context(), backuptypes.RunSystemVolumeBackupsRequest{})
	require.ErrorIs(t, err, ErrSystemBackupAlreadyRunning)
}
