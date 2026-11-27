package backup

import (
	"testing"

	mobyContainer "github.com/moby/moby/api/types/container"
	mobyMount "github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestBackupMountWarningFromArcaneMounts(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		mounts []mobyContainer.MountPoint
	}{
		{
			name: "bind mount at backups suppresses warning",
			mounts: []mobyContainer.MountPoint{
				{Type: mobyMount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "named volume at backups suppresses warning",
			mounts: []mobyContainer.MountPoint{
				{Type: mobyMount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "bind mount at restores suppresses warning",
			mounts: []mobyContainer.MountPoint{
				{Type: mobyMount.TypeBind, Source: "/host/restores", Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "unsupported restores mount still suppresses warning for compatibility",
			mounts: []mobyContainer.MountPoint{
				{Type: mobyMount.TypeTmpfs, Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "missing backups mount warns",
			mounts: []mobyContainer.MountPoint{
				{Type: mobyMount.TypeBind, Source: "/host/other", Destination: "/other"},
			},
			want: backupMountMissingWarning,
		},
		{
			name: "unsupported backups mount type warns",
			mounts: []mobyContainer.MountPoint{
				{Type: mobyMount.TypeTmpfs, Destination: "/backups"},
			},
			want: backupMountMissingWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, backupMountWarningFromArcaneMounts(tt.mounts))
		})
	}
}
