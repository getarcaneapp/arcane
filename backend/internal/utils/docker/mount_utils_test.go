package docker

import (
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestMountForDestination(t *testing.T) {
	tests := []struct {
		name       string
		mounts     []containertypes.MountPoint
		dest       string
		target     string
		wantNil    bool
		wantType   mounttypes.Type
		wantSource string
		wantTarget string
		wantRO     bool
	}{
		{
			name: "returns bind mount",
			mounts: []containertypes.MountPoint{
				{Type: mounttypes.TypeBind, Source: "/host/backups", Destination: "/backups", RW: true},
			},
			dest:       "/backups",
			target:     "/volume",
			wantType:   mounttypes.TypeBind,
			wantSource: "/host/backups",
			wantTarget: "/volume",
			wantRO:     false,
		},
		{
			name: "returns named volume mount",
			mounts: []containertypes.MountPoint{
				{Type: mounttypes.TypeVolume, Name: "arcane-backups", Destination: "/backups", RW: false},
			},
			dest:       "/backups",
			target:     "/restores",
			wantType:   mounttypes.TypeVolume,
			wantSource: "arcane-backups",
			wantTarget: "/restores",
			wantRO:     true,
		},
		{
			name: "defaults target to destination",
			mounts: []containertypes.MountPoint{
				{Type: mounttypes.TypeBind, Source: "/host/backups", Destination: "/backups", RW: true},
			},
			dest:       "/backups",
			wantType:   mounttypes.TypeBind,
			wantSource: "/host/backups",
			wantTarget: "/backups",
			wantRO:     false,
		},
		{
			name: "ignores unsupported mount types",
			mounts: []containertypes.MountPoint{
				{Type: mounttypes.TypeTmpfs, Destination: "/backups"},
			},
			dest:    "/backups",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MountForDestination(tt.mounts, tt.dest, tt.target)
			if tt.wantNil {
				require.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			require.Equal(t, tt.wantType, got.Type)
			require.Equal(t, tt.wantSource, got.Source)
			require.Equal(t, tt.wantTarget, got.Target)
			require.Equal(t, tt.wantRO, got.ReadOnly)
		})
	}
}
