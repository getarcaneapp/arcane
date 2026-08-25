package imagepatch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePatchedRef(t *testing.T) {
	tests := []struct {
		name       string
		imageRef   string
		patchedTag string
		suffix     string
		want       string
	}{
		{
			name:     "default suffix",
			imageRef: "nginx:1.25",
			suffix:   "patched",
			want:     "docker.io/library/nginx:1.25-patched",
		},
		{
			name:       "explicit tag override with registry port",
			imageRef:   "registry.local:5000/app:2.0",
			patchedTag: "2.0-hardened",
			suffix:     "patched",
			want:       "registry.local:5000/app:2.0-hardened",
		},
		{
			name:     "custom suffix",
			imageRef: "ghcr.io/acme/api:v3",
			suffix:   "fixed",
			want:     "ghcr.io/acme/api:v3-fixed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePatchedRef(tt.imageRef, tt.patchedTag, tt.suffix)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
