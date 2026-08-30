package systembackup

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

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
		Filters: map[string]string{"type": "system"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, systemRows, 2)
	require.Equal(t, backuptypes.ManagementTypeSystem, systemRows[0].Type)
	require.Equal(t, "app-data", systemRows[0].ResourceName)
	require.Equal(t, "volume", systemRows[0].ResourceType)

	volumeRows, page, err := service.ListBackupHistory(context.Background(), pagination.QueryParams{
		Search: "cache", Sort: "createdAt", Order: pagination.SortDesc, Limit: 1,
		Filters: map[string]string{"type": "volume"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, volumeRows, 1)
	require.Equal(t, backuptypes.ManagementTypeVolume, volumeRows[0].Type)
	require.Equal(t, "cache", volumeRows[0].ResourceName)
}
