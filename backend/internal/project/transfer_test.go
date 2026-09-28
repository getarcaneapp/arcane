package project

import (
	"testing"

	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteTransferredComposeInternal(t *testing.T) {
	source := []byte(`name: old-app

services:
  app:
    image: nginx:alpine
    volumes:
      - /srv/old/data:/data:ro
      - type: bind
        source: /srv/old/config
        target: /config
      - data:/var/lib/data
      - ./local:/local

volumes:
  data:
    name: "old-app-data"
  shared:
    external: true
  keep:
    name: keep-me
`)

	updated, err := rewriteTransferredComposeInternal(source, transfertypes.ProjectRewrite{
		Name:           "new-app",
		SourcePath:     "/srv/old",
		VolumeMappings: map[string]string{"old-app-data": "new-app-data", "shared": "shared-new"},
	}, "/srv/new")
	require.NoError(t, err)

	expected := `name: new-app

services:
  app:
    image: nginx:alpine
    volumes:
      - /srv/new/data:/data:ro
      - type: bind
        source: /srv/new/config
        target: /config
      - data:/var/lib/data
      - ./local:/local

volumes:
  data:
    name: "new-app-data"
  shared:
    name: shared-new
    external: true
  keep:
    name: keep-me
`
	assert.Equal(t, expected, string(updated))

	unchanged, err := rewriteTransferredComposeInternal(source, transfertypes.ProjectRewrite{Name: "old-app"}, "")
	require.NoError(t, err)
	assert.Equal(t, string(source), string(unchanged))

	_, err = rewriteTransferredComposeInternal([]byte("services: {app: {image: x}}\nvolumes: {shared: {external: true}}\n"), transfertypes.ProjectRewrite{Name: "x", VolumeMappings: map[string]string{"shared": "renamed"}}, "")
	require.Error(t, err, "flow-style external volumes cannot receive a name")
}
