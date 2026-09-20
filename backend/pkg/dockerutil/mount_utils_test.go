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

func TestMountForSubpath(t *testing.T) {
	hostBind := containertypes.MountPoint{Type: mounttypes.TypeBind, Source: "/host/projects-root", Destination: "/app/data", RW: true}
	namedVolume := containertypes.MountPoint{Type: mounttypes.TypeVolume, Name: "arcane-dev-data", Destination: "/app/data", RW: true}
	nestedBind := containertypes.MountPoint{Type: mounttypes.TypeBind, Source: "/host/projects-only", Destination: "/app/data/projects", RW: true}
	readOnlyVolume := containertypes.MountPoint{Type: mounttypes.TypeVolume, Name: "ro-vol", Destination: "/app/data", RW: false}
	tmpfsMount := containertypes.MountPoint{Type: mounttypes.TypeTmpfs, Destination: "/app/data"}

	t.Run("bind mount with subpath joins host source", func(t *testing.T) {
		got := MountForSubpath([]containertypes.MountPoint{hostBind}, "/app/data/projects/foo", "/workspace")
		require.NotNil(t, got)
		require.Equal(t, mounttypes.TypeBind, got.Type)
		require.Equal(t, "/host/projects-root/projects/foo", got.Source)
		require.Equal(t, "/workspace", got.Target)
		require.False(t, got.ReadOnly)
		require.Nil(t, got.VolumeOptions)
	})

	t.Run("named volume with subpath uses VolumeOptions.Subpath", func(t *testing.T) {
		got := MountForSubpath([]containertypes.MountPoint{namedVolume}, "/app/data/projects/foo", "/workspace")
		require.NotNil(t, got)
		require.Equal(t, mounttypes.TypeVolume, got.Type)
		require.Equal(t, "arcane-dev-data", got.Source)
		require.Equal(t, "/workspace", got.Target)
		require.NotNil(t, got.VolumeOptions)
		require.Equal(t, "projects/foo", got.VolumeOptions.Subpath)
	})

	t.Run("picks the most-specific matching mount", func(t *testing.T) {
		got := MountForSubpath([]containertypes.MountPoint{hostBind, nestedBind}, "/app/data/projects/foo", "/workspace")
		require.NotNil(t, got)
		// nestedBind's destination is more specific, so the relative subpath is "foo"
		require.Equal(t, "/host/projects-only/foo", got.Source)
	})

	t.Run("exact-destination match has no subpath", func(t *testing.T) {
		got := MountForSubpath([]containertypes.MountPoint{namedVolume}, "/app/data", "/workspace")
		require.NotNil(t, got)
		require.Equal(t, mounttypes.TypeVolume, got.Type)
		require.Equal(t, "arcane-dev-data", got.Source)
		require.Nil(t, got.VolumeOptions, "exact destination match shouldn't add VolumeOptions")
	})

	t.Run("preserves read-only", func(t *testing.T) {
		got := MountForSubpath([]containertypes.MountPoint{readOnlyVolume}, "/app/data/projects/foo", "/workspace")
		require.NotNil(t, got)
		require.True(t, got.ReadOnly)
	})

	t.Run("defaults target to containerPath", func(t *testing.T) {
		got := MountForSubpath([]containertypes.MountPoint{hostBind}, "/app/data/projects/foo", "")
		require.NotNil(t, got)
		require.Equal(t, "/app/data/projects/foo", got.Target)
	})

	t.Run("rejects unsupported mount types", func(t *testing.T) {
		require.Nil(t, MountForSubpath([]containertypes.MountPoint{tmpfsMount}, "/app/data/projects/foo", "/workspace"))
	})

	t.Run("returns nil when no mount destination is a prefix", func(t *testing.T) {
		require.Nil(t, MountForSubpath([]containertypes.MountPoint{hostBind}, "/elsewhere/foo", "/workspace"))
	})

	t.Run("does not match similar-looking destinations", func(t *testing.T) {
		// "/app/datax" must not match "/app/data".
		require.Nil(t, MountForSubpath([]containertypes.MountPoint{hostBind}, "/app/datax/foo", "/workspace"))
	})

	t.Run("rejects empty containerPath", func(t *testing.T) {
		require.Nil(t, MountForSubpath([]containertypes.MountPoint{hostBind}, "", "/workspace"))
	})

	t.Run("rejects bind mount with empty source", func(t *testing.T) {
		bad := containertypes.MountPoint{Type: mounttypes.TypeBind, Destination: "/app/data"}
		require.Nil(t, MountForSubpath([]containertypes.MountPoint{bad}, "/app/data/projects/foo", "/workspace"))
	})

	t.Run("rejects named volume with empty name", func(t *testing.T) {
		bad := containertypes.MountPoint{Type: mounttypes.TypeVolume, Destination: "/app/data"}
		require.Nil(t, MountForSubpath([]containertypes.MountPoint{bad}, "/app/data/projects/foo", "/workspace"))
	})
}

func TestPreserveVolumeMounts(t *testing.T) {
	anon := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mountPoints := []containertypes.MountPoint{
		{Type: mounttypes.TypeVolume, Name: anon, Destination: "/app/data", RW: true},
		{Type: mounttypes.TypeVolume, Name: "named", Destination: "/data", RW: true},
		{Type: mounttypes.TypeVolume, Name: "ro", Destination: "/ro", RW: false},
		{Type: mounttypes.TypeBind, Source: "/host", Destination: "/bind", RW: true},
	}

	tests := []struct {
		name       string
		binds      []string
		mounts     []mounttypes.Mount
		points     []containertypes.MountPoint
		wantBinds  []string
		wantMounts []mounttypes.Mount
		wantErr    bool
	}{
		{
			name:   "image-declared volume gains explicit mount",
			points: mountPoints[:1],
			wantMounts: []mounttypes.Mount{
				{Type: mounttypes.TypeVolume, Source: anon, Target: "/app/data"},
			},
		},
		{
			name:      "anonymous bind gains volume name, explicit binds preserved",
			binds:     []string{"/app/data", "named:/data", "/host:/bind:ro"},
			points:    mountPoints,
			wantBinds: []string{anon + ":/app/data", "named:/data", "/host:/bind:ro"},
			wantMounts: []mounttypes.Mount{
				{Type: mounttypes.TypeVolume, Source: "ro", Target: "/ro", ReadOnly: true},
			},
		},
		{
			name: "structured mount keeps options and gets source",
			mounts: []mounttypes.Mount{
				{Type: mounttypes.TypeVolume, Target: "/app/data/", VolumeOptions: &mounttypes.VolumeOptions{Subpath: "sub"}},
				{Type: mounttypes.TypeTmpfs, Target: "/tmp"},
			},
			points: mountPoints[:1],
			wantMounts: []mounttypes.Mount{
				{Type: mounttypes.TypeVolume, Source: anon, Target: "/app/data/", VolumeOptions: &mounttypes.VolumeOptions{Subpath: "sub"}},
				{Type: mounttypes.TypeTmpfs, Target: "/tmp"},
			},
		},
		{
			name:    "volume without name fails",
			points:  []containertypes.MountPoint{{Type: mounttypes.TypeVolume, Destination: "/app/data"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origBinds := append([]string(nil), tt.binds...)
			origMounts := append([]mounttypes.Mount(nil), tt.mounts...)

			binds, mounts, err := PreserveVolumeMounts(tt.binds, tt.mounts, tt.points)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantBinds, binds)
			require.Equal(t, tt.wantMounts, mounts)
			require.Equal(t, origBinds, tt.binds)
			require.Equal(t, origMounts, tt.mounts)
		})
	}
}
