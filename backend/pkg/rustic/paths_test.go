package rustic

import (
	"testing"

	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestWindowsContainerPathsInternal(t *testing.T) {
	mounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: "arcane-backups", Target: "/repository"},
		{Type: mount.TypeVolume, Source: "data", Target: "/volume", ReadOnly: true},
		{Type: mount.TypeVolume, Source: CacheVolume, Target: "/cache"},
	}
	env := []string{"RUSTIC_REPOSITORY=/repository/volumes", "RUSTIC_CACHE_DIR=/cache", "RUSTIC_PASSWORD=secret", "RUSTIC_REPOSITORY_OPTIONS=/volumes-not-a-mount"}
	command := []string{"restore", "--delete", "--", "abc123:/dir/", "/volume/dir", "--as-path", "/", "/volumes"}

	gotCommand, gotEnv, gotMounts := windowsContainerPathsInternal(command, env, mounts)

	require.Equal(t, []string{"restore", "--delete", "--", "abc123:/dir/", `C:\volume\dir`, "--as-path", "/", "/volumes"}, gotCommand)
	require.Equal(t, []string{`RUSTIC_REPOSITORY=C:\repository\volumes`, `RUSTIC_CACHE_DIR=C:\cache`, "RUSTIC_PASSWORD=secret", "RUSTIC_REPOSITORY_OPTIONS=/volumes-not-a-mount"}, gotEnv)
	require.Equal(t, `C:\repository`, gotMounts[0].Target)
	require.Equal(t, `C:\volume`, gotMounts[1].Target)
	require.True(t, gotMounts[1].ReadOnly)
	require.Equal(t, `C:\cache`, gotMounts[2].Target)
	require.Equal(t, "/volume", mounts[1].Target)
	require.Equal(t, "/volume/dir", command[4])
}
