package volume

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListBackupsPaginatedByManagementTypeInternal(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-management?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackup{}))
	for _, entry := range []*VolumeBackup{
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: volumetypes.BackupDestinationLocal, PolicyID: backuptypes.SystemVolumePolicyPrefix + "abc"},
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: volumetypes.BackupDestinationLocal, PolicyID: "per-volume"},
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: volumetypes.BackupDestinationLocal},
	} {
		require.NoError(t, gormDB.Create(entry).Error)
	}
	service := &VolumeService{db: &database.DB{DB: gormDB}}

	systemRows, page, err := service.ListBackupsPaginated(context.Background(), "app", pagination.QueryParams{
		Filters: map[string]string{"type": "system"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, systemRows, 1)
	require.Equal(t, backuptypes.ManagementTypeSystem, systemRows[0].Type)

	volumeRows, page, err := service.ListBackupsPaginated(context.Background(), "app", pagination.QueryParams{
		Filters: map[string]string{"type": "volume"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, volumeRows, 2)
	for _, entry := range volumeRows {
		require.Equal(t, backuptypes.ManagementTypeVolume, entry.Type)
	}
}
