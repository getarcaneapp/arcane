package libarcane

import (
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	systemtypes "github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestPrepareRecreateHostConfigForEngine_NilHostConfig(t *testing.T) {
	out, sanitized, engineInfo, err := PrepareRecreateHostConfigForEngine(t.Context(), nil, nil)
	require.NoError(t, err)
	require.Nil(t, out)
	require.False(t, sanitized)
	require.Empty(t, engineInfo.Name)
	require.Empty(t, engineInfo.CgroupVersion)
}

func TestSanitizeRecreateHostConfigInternal(t *testing.T) {
	swappiness := int64(60)

	tests := []struct {
		name              string
		engineInfo        EngineCompatibilityInfo
		wantSanitized     bool
		wantSwappinessNil bool
	}{
		{
			name: "podman cgroup v2 strips memory swappiness",
			engineInfo: EngineCompatibilityInfo{
				Name:          "podman",
				CgroupVersion: "2",
			},
			wantSanitized:     true,
			wantSwappinessNil: true,
		},
		{
			name: "podman cgroup v1 preserves memory swappiness",
			engineInfo: EngineCompatibilityInfo{
				Name:          "podman",
				CgroupVersion: "1",
			},
			wantSanitized:     false,
			wantSwappinessNil: false,
		},
		{
			name: "docker cgroup v2 preserves memory swappiness",
			engineInfo: EngineCompatibilityInfo{
				Name:          "docker",
				CgroupVersion: "2",
			},
			wantSanitized:     false,
			wantSwappinessNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &containertypes.HostConfig{
				Resources: containertypes.Resources{
					MemorySwappiness: &swappiness,
					CPUShares:        1024,
				},
				PublishAllPorts: true,
				ReadonlyRootfs:  true,
				NetworkMode:     "bridge",
				ContainerIDFile: "/tmp/id",
				VolumeDriver:    "local",
				Annotations:     map[string]string{"test": "value"},
				MaskedPaths:     []string{"/proc/kcore"},
				ReadonlyPaths:   []string{"/proc/asound"},
			}

			cloned := cloneContainerHostConfigInternal(input)
			require.NotNil(t, cloned)
			require.NotSame(t, input, cloned)

			sanitized := sanitizeRecreateHostConfigInternal(cloned, tt.engineInfo)
			require.Equal(t, tt.wantSanitized, sanitized)

			if tt.wantSwappinessNil {
				require.Nil(t, cloned.MemorySwappiness)
			} else {
				require.NotNil(t, cloned.MemorySwappiness)
				require.Equal(t, int64(60), *cloned.MemorySwappiness)
			}

			require.NotNil(t, input.MemorySwappiness)
			require.Equal(t, int64(60), *input.MemorySwappiness)
			require.Equal(t, int64(1024), cloned.CPUShares)
			require.True(t, cloned.PublishAllPorts)
			require.True(t, cloned.ReadonlyRootfs)
			require.Equal(t, containertypes.NetworkMode("bridge"), cloned.NetworkMode)
			require.Equal(t, "/tmp/id", cloned.ContainerIDFile)
			require.Equal(t, "local", cloned.VolumeDriver)
			require.Equal(t, map[string]string{"test": "value"}, cloned.Annotations)
			require.Equal(t, []string{"/proc/kcore"}, cloned.MaskedPaths)
			require.Equal(t, []string{"/proc/asound"}, cloned.ReadonlyPaths)
		})
	}
}

func TestSanitizeRecreateHostConfigInternal_NilHostConfig(t *testing.T) {
	sanitized := sanitizeRecreateHostConfigInternal(nil, EngineCompatibilityInfo{Name: "podman", CgroupVersion: "2"})
	require.False(t, sanitized)
}

func TestDetectEngineCompatibilityInfoInternal(t *testing.T) {
	t.Run("prefers platform name for podman detection", func(t *testing.T) {
		version := client.ServerVersionResult{}
		version.Platform.Name = "Podman Engine"
		info := systemtypes.Info{CgroupVersion: "2"}

		engineInfo := detectEngineCompatibilityInfoInternal(version, info)
		require.Equal(t, "podman", engineInfo.Name)
		require.Equal(t, "2", engineInfo.CgroupVersion)
	})

	t.Run("detects podman from component names", func(t *testing.T) {
		version := client.ServerVersionResult{
			Components: []systemtypes.ComponentVersion{
				{Name: "Podman Engine"},
			},
		}
		info := systemtypes.Info{CgroupVersion: "2"}

		engineInfo := detectEngineCompatibilityInfoInternal(version, info)
		require.Equal(t, "podman", engineInfo.Name)
		require.Equal(t, "2", engineInfo.CgroupVersion)
	})

	t.Run("falls back to docker markers", func(t *testing.T) {
		version := client.ServerVersionResult{}
		info := systemtypes.Info{
			CgroupVersion: "2",
			ServerVersion: "Docker Engine - Community",
		}

		engineInfo := detectEngineCompatibilityInfoInternal(version, info)
		require.Equal(t, "docker", engineInfo.Name)
		require.Equal(t, "2", engineInfo.CgroupVersion)
	})
}
