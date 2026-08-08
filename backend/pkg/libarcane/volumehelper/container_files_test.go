package volumehelper

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
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
			require.Equal(t, tt.want, IsLegacyHelperContainer(tt.summary))
		})
	}
}

func TestIsVolumeHelperContainerInternal_UsesExplicitHelperLabel(t *testing.T) {
	tests := []struct {
		name    string
		summary container.Summary
		want    bool
	}{
		{
			name: "new helper label matches",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
					ContainerLabel:                  "true",
				},
			},
			want: true,
		},
		{
			name: "generic internal volume mount does not match",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: false,
		},
		{
			name: "legacy helper still matches",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsHelperContainer(tt.summary))
		})
	}
}
