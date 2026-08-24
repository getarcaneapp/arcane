package volume

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	mobyvolume "github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListBackupsPaginatedByTypeInternal(t *testing.T) {
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

	systemRows, page, err := service.ListBackupsPaginatedByType(context.Background(), "app", pagination.QueryParams{}, "system")
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, systemRows, 1)
	require.Equal(t, backuptypes.ManagementTypeSystem, systemRows[0].Type)

	volumeRows, page, err := service.ListBackupsPaginatedByType(context.Background(), "app", pagination.QueryParams{}, "volume")
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, volumeRows, 2)
	for _, entry := range volumeRows {
		require.Equal(t, backuptypes.ManagementTypeVolume, entry.Type)
	}
}

func TestIsAnonymousVolumeInternal(t *testing.T) {
	require.True(t, isAnonymousVolumeInternal(mobyvolume.Volume{Name: "named", Labels: map[string]string{"com.docker.volume.anonymous": ""}}))
	require.True(t, isAnonymousVolumeInternal(mobyvolume.Volume{Name: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}))
	require.False(t, isAnonymousVolumeInternal(mobyvolume.Volume{Name: "app-data"}))
}

func TestMountedVolumeNamesInternal(t *testing.T) {
	names := mountedVolumeNamesInternal([]container.MountPoint{
		{Type: mount.TypeVolume, Name: "arcane-data"},
		{Type: mount.TypeVolume, Source: "legacy-volume"},
		{Type: mount.TypeBind, Source: "/host/projects"},
	})
	require.Contains(t, names, "arcane-data")
	require.Contains(t, names, "legacy-volume")
	require.NotContains(t, names, "/host/projects")
}
