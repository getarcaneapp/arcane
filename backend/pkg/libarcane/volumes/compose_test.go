package volumes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeVolumeKeysWithExplicitNameInternal(t *testing.T) {
	projectPath := t.TempDir()
	rootCompose := filepath.Join(projectPath, "compose.yaml")
	includeCompose := filepath.Join(projectPath, "included.yaml")

	require.NoError(t, os.WriteFile(rootCompose, []byte(`
services:
  app:
    image: nginx:alpine
    volumes:
      - data:/data
      - fixed:/fixed
      - env_named:/env
      - env_default:/env-default
      - env_unbraced:/env-unbraced
      - escaped:/escaped
      - scalar:/scalar
      - null_named:/null
      - numeric_named:/numeric
volumes:
  data:
    driver: local
  fixed:
    name: app-data
  env_named:
    name: ${APP_VOLUME}
  env_default:
    name: ${APP_VOLUME:-app-data}
  env_unbraced:
    name: $APP_VOLUME
  escaped:
    name: $${APP_VOLUME}
  scalar:
  null_named:
    name: null
  numeric_named:
    name: 123
  inline: {}
`), 0o644))
	require.NoError(t, os.WriteFile(includeCompose, []byte(`
volumes:
  included:
    name: included-fixed
  included_implicit:
    driver: local
`), 0o644))

	explicit, err := composeVolumeKeysWithExplicitNameInternal([]string{rootCompose, includeCompose})
	require.NoError(t, err)

	assert.Contains(t, explicit, "fixed")
	assert.Contains(t, explicit, "escaped")
	assert.Contains(t, explicit, "included")
	assert.NotContains(t, explicit, "data")
	assert.NotContains(t, explicit, "env_named")
	assert.NotContains(t, explicit, "env_default")
	assert.NotContains(t, explicit, "env_unbraced")
	assert.NotContains(t, explicit, "scalar")
	assert.NotContains(t, explicit, "null_named")
	assert.NotContains(t, explicit, "numeric_named")
	assert.NotContains(t, explicit, "inline")
	assert.NotContains(t, explicit, "included_implicit")
}
