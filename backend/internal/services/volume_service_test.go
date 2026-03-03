package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	volumetypes "github.com/getarcaneapp/arcane/types/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/require"
)

func TestIsLegacyVolumeHelperContainerInternal(t *testing.T) {
	tests := []struct {
		name    string
		summary container.Summary
		want    bool
	}{
		{
			name: "legacy helper signature matches",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
				Command: "sleep infinity",
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: true,
		},
		{
			name: "internal trivy-like helper is not treated as legacy volume helper",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
				Command: "trivy image --quiet alpine:latest",
				Mounts: []container.MountPoint{
					{Destination: "/var/run/docker.sock"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isLegacyVolumeHelperContainerInternal(tt.summary))
		})
	}
}

func TestBuildVolumeHelperLabelsInternal(t *testing.T) {
	labels := buildVolumeHelperLabelsInternal()

	require.Equal(t, "true", labels[libarcane.InternalResourceLabel])
	require.Len(t, labels, 1)
}

func TestEnrichVolumesWithUsageDataInternal(t *testing.T) {
	svc := &VolumeService{}

	tests := []struct {
		name         string
		volumes      []volume.Volume
		usageVolumes []volume.Volume
		wantLen      int
		assertions   func(t *testing.T, got []volume.Volume)
	}{
		{
			name: "attaches usage by name",
			volumes: []volume.Volume{
				{Name: "vol-a"},
				{Name: "vol-b"},
			},
			usageVolumes: []volume.Volume{
				{Name: "vol-a", UsageData: &volume.UsageData{Size: 100, RefCount: 2}},
				{Name: "vol-c", UsageData: &volume.UsageData{Size: 50, RefCount: 1}},
			},
			wantLen: 2,
			assertions: func(t *testing.T, got []volume.Volume) {
				require.NotNil(t, got[0].UsageData)
				require.EqualValues(t, 100, got[0].UsageData.Size)
				require.EqualValues(t, 2, got[0].UsageData.RefCount)
				require.Nil(t, got[1].UsageData)
			},
		},
		{
			name: "keeps first usage entry when duplicate usage names exist",
			volumes: []volume.Volume{
				{Name: "vol-dup"},
			},
			usageVolumes: []volume.Volume{
				{Name: "vol-dup", UsageData: &volume.UsageData{Size: 10, RefCount: 1}},
				{Name: "vol-dup", UsageData: &volume.UsageData{Size: 20, RefCount: 3}},
			},
			wantLen: 1,
			assertions: func(t *testing.T, got []volume.Volume) {
				require.NotNil(t, got[0].UsageData)
				require.EqualValues(t, 10, got[0].UsageData.Size)
				require.EqualValues(t, 1, got[0].UsageData.RefCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.enrichVolumesWithUsageDataInternal(tt.volumes, tt.usageVolumes)
			require.Len(t, got, tt.wantLen)
			tt.assertions(t, got)
		})
	}
}

func TestIsInternalVolumeInternal(t *testing.T) {
	svc := &VolumeService{backupVolumeName: "arcane-backups"}

	require.True(t, svc.isInternalVolumeInternal(volumetypes.Volume{Name: "arcane-backups"}))
	require.False(t, svc.isInternalVolumeInternal(volumetypes.Volume{Name: "user-volume"}))
}

func TestResolveBackupStorageMountFromMountsInternal(t *testing.T) {
	tests := []struct {
		name         string
		mounts       []container.MountPoint
		target       string
		readOnly     bool
		wantResolved bool
		wantMode     backupStorageMode
		wantType     mount.Type
		wantSource   string
		wantTarget   string
		wantReadOnly bool
		wantEnsure   bool
	}{
		{
			name: "mirrors bind mount",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			target:       "/volume",
			readOnly:     true,
			wantResolved: true,
			wantMode:     backupStorageModeArcaneMount,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: true,
		},
		{
			name: "writable request against read-only bind mount still resolves",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups", RW: false},
			},
			target:       "/volume",
			readOnly:     false,
			wantResolved: true,
			wantMode:     backupStorageModeArcaneMount,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: false,
		},
		{
			name: "mirrors named volume",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     false,
			wantResolved: true,
			wantMode:     backupStorageModeArcaneMount,
			wantType:     mount.TypeVolume,
			wantSource:   "arcane-backups",
			wantTarget:   "/backups",
			wantReadOnly: false,
		},
		{
			name: "ignores unsupported mount types",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
		{
			name:         "returns unresolved when mount is absent",
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveBackupStorageMountFromMountsInternal(tt.mounts, tt.target, tt.readOnly)
			require.Equal(t, tt.wantResolved, ok)
			if !tt.wantResolved {
				return
			}

			require.Equal(t, tt.wantMode, got.mode)
			require.Equal(t, tt.wantType, got.mount.Type)
			require.Equal(t, tt.wantSource, got.mount.Source)
			require.Equal(t, tt.wantTarget, got.mount.Target)
			require.Equal(t, tt.wantReadOnly, got.mount.ReadOnly)
			require.Equal(t, tt.wantEnsure, got.requiresEnsure)
		})
	}
}

func TestResolveBackupStorageMountInternalFallsBackToNamedVolume(t *testing.T) {
	svc := &VolumeService{backupVolumeName: "arcane-backups"}

	got, err := svc.resolveBackupStorageMountInternal(context.Background(), nil, "/backups", true)
	require.NoError(t, err)
	require.Equal(t, backupStorageModeNamedVolumeFallback, got.mode)
	require.Equal(t, mount.TypeVolume, got.mount.Type)
	require.Equal(t, "arcane-backups", got.mount.Source)
	require.Equal(t, "/backups", got.mount.Target)
	require.True(t, got.mount.ReadOnly)
	require.True(t, got.requiresEnsure)
}

func TestBackupMountWarningForStorageInternal(t *testing.T) {
	require.Empty(t, backupMountWarningForStorageInternal(backupStorageMountInternal{mode: backupStorageModeArcaneMount}))
	require.Equal(t, backupMountMissingWarning, backupMountWarningForStorageInternal(backupStorageMountInternal{mode: backupStorageModeNamedVolumeFallback}))
}

func TestBackupMountWarningFromArcaneMountsInternal(t *testing.T) {
	tests := []struct {
		name   string
		mounts []container.MountPoint
		want   string
	}{
		{
			name: "bind mount at backups suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "named volume at backups suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "bind mount at restores suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/restores", Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "unsupported restores mount still suppresses warning for compatibility",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "missing backups mount warns",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/other", Destination: "/other"},
			},
			want: backupMountMissingWarning,
		},
		{
			name: "unsupported backups mount type warns",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/backups"},
			},
			want: backupMountMissingWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, backupMountWarningFromArcaneMountsInternal(tt.mounts))
		})
	}
}

func TestBackupArchiveFilenameInternal(t *testing.T) {
	svc := &VolumeService{}

	tests := []struct {
		name     string
		backupID string
		want     string
		wantErr  bool
	}{
		{
			name:     "valid backup id",
			backupID: "volume-123-abc",
			want:     "volume-123-abc.tar.gz",
		},
		{
			name:     "trims surrounding whitespace",
			backupID: "  volume-123-abc  ",
			want:     "volume-123-abc.tar.gz",
		},
		{
			name:     "rejects traversal attempts",
			backupID: "../../bin/busybox",
			wantErr:  true,
		},
		{
			name:     "rejects path separators",
			backupID: "nested/path",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.backupArchiveFilenameInternal(tt.backupID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
