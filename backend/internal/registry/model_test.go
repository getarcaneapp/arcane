package registry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"kit.getarcane.app/mapping"

	"getarcane.dev/types/v3"
)

func TestContainerRegistry_MapsHostAndNamespace(t *testing.T) {
	tests := []struct {
		url       string
		host      string
		namespace string
	}{
		{url: "https://GHCR.io/acme/", host: "ghcr.io", namespace: "acme"},
		{url: "https://index.docker.io/v1/", host: "docker.io", namespace: ""},
		{url: "registry.example.com", host: "registry.example.com", namespace: ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			out, err := mapping.MapOne[*ContainerRegistry, types.ContainerRegistry](&ContainerRegistry{URL: tt.url})
			require.NoError(t, err)
			require.Equal(t, tt.host, out.Host)
			require.Equal(t, tt.namespace, out.Namespace)
		})
	}
}
