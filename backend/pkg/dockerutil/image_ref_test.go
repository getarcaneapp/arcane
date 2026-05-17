package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplaceImageTag(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		tag      string
		want     string
	}{
		{
			name:     "replace existing tag",
			imageRef: "ghcr.io/getarcaneapp/arcane:latest",
			tag:      "v1.19.3",
			want:     "ghcr.io/getarcaneapp/arcane:v1.19.3",
		},
		{
			name:     "add missing tag",
			imageRef: "ghcr.io/getarcaneapp/arcane",
			tag:      "v1.19.3",
			want:     "ghcr.io/getarcaneapp/arcane:v1.19.3",
		},
		{
			name:     "preserve registry port",
			imageRef: "localhost:5000/getarcaneapp/arcane:latest",
			tag:      "v1.19.3",
			want:     "localhost:5000/getarcaneapp/arcane:v1.19.3",
		},
		{
			name:     "strip digest",
			imageRef: "ghcr.io/getarcaneapp/arcane:latest@sha256:abc123",
			tag:      "v1.19.3",
			want:     "ghcr.io/getarcaneapp/arcane:v1.19.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ReplaceImageTag(tt.imageRef, tt.tag))
		})
	}
}
