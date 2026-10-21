package volumehelper

import (
	"testing"

	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestWindowsMountsInternal(t *testing.T) {
	binds := []string{"data:/volume", "data:/volume:ro", `data:C:\volume`}
	mounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: "arcane-backups", Target: "/backups", ReadOnly: true},
		{Type: mount.TypeVolume, Source: "cache", Target: `C:\cache`},
	}

	gotBinds, gotMounts := windowsMountsInternal(binds, mounts)

	require.Equal(t, []string{`data:C:\volume`, `data:C:\volume:ro`, `data:C:\volume`}, gotBinds)
	require.Equal(t, `C:\backups`, gotMounts[0].Target)
	require.True(t, gotMounts[0].ReadOnly)
	require.Equal(t, `C:\cache`, gotMounts[1].Target)
	require.Equal(t, "data:/volume", binds[0])
	require.Equal(t, "/backups", mounts[0].Target)
}

func TestWindowsContainerPath(t *testing.T) {
	require.Equal(t, `C:\tmp\arcane-workspace`, WindowsContainerPath("/tmp/arcane-workspace"))
	require.Equal(t, "abc123:/dir", WindowsContainerPath("abc123:/dir"))
}
