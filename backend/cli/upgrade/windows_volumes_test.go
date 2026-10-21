package upgrade

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestPreserveWindowsVolumeBindsInternal(t *testing.T) {
	pipe := `\\.\pipe\docker_engine:\\.\pipe\docker_engine`
	mountPoints := []container.MountPoint{
		{Type: mount.TypeNamedPipe, Source: `\\.\pipe\docker_engine`, Destination: `\\.\pipe\docker_engine`},
		{Type: mount.TypeVolume, Name: "arcane-agent-data", Destination: `c:\app\data`},
	}

	t.Run("named volume bind is kept as is", func(t *testing.T) {
		binds, mounts := preserveWindowsVolumeBindsInternal([]string{pipe, `arcane-agent-data:C:\app\data`}, nil, mountPoints)
		require.Equal(t, []string{pipe, `arcane-agent-data:C:\app\data`}, binds)
		require.Empty(t, mounts)
	})

	t.Run("anonymous volume bind gets its name", func(t *testing.T) {
		binds, _ := preserveWindowsVolumeBindsInternal([]string{pipe, `C:\app\data`}, nil, mountPoints)
		require.Equal(t, []string{pipe, `arcane-agent-data:C:\app\data`}, binds)
	})

	t.Run("image-declared volume becomes a bind", func(t *testing.T) {
		binds, mounts := preserveWindowsVolumeBindsInternal([]string{pipe}, nil, mountPoints)
		require.Equal(t, []string{pipe, `arcane-agent-data:c:\app\data`}, binds)
		require.Empty(t, mounts)
	})

	t.Run("volume given through mounts keeps its mount", func(t *testing.T) {
		in := []mount.Mount{{Type: mount.TypeVolume, Source: "old", Target: `C:\app\data`}}
		binds, mounts := preserveWindowsVolumeBindsInternal([]string{pipe}, in, mountPoints)
		require.Equal(t, []string{pipe}, binds)
		require.Equal(t, "arcane-agent-data", mounts[0].Source)
		require.Equal(t, "old", in[0].Source)
	})
}

func TestSplitWindowsBindInternal(t *testing.T) {
	require.Equal(t, []string{"vol", `C:\app\data`, "ro"}, splitWindowsBindInternal(`vol:C:\app\data:ro`))
	require.Equal(t, []string{`D:\host`, `C:\app\data`}, splitWindowsBindInternal(`D:\host:C:\app\data`))
	require.Equal(t, []string{`C:\app\data`}, splitWindowsBindInternal(`C:\app\data`))
}
