package services

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
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
