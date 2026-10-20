package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"getarcane.dev/arcane/v3/internal/secretsource"
)

func TestMaskEnvValuesMasksOnlySecretKeys(t *testing.T) {
	env := []string{"DB_PASSWORD=hunter22", "MODE=prod", "API_KEY=abc", "FLAG"}
	masked := maskEnvValuesInternal(env, []string{"DB_PASSWORD", "API_KEY", "FLAG"})
	assert.Equal(t, []string{"DB_PASSWORD", "API_KEY"}, masked, "an entry without a value has nothing to mask")
	assert.Equal(t, []string{"DB_PASSWORD=" + secretsource.RedactedMarker, "MODE=prod", "API_KEY=" + secretsource.RedactedMarker, "FLAG"}, env)

	assert.Nil(t, maskEnvValuesInternal([]string{"A=1"}, nil))
}

func TestMaskSecretEnvSkipsContainersOutsideCompose(t *testing.T) {
	service := &ContainerService{}
	env := []string{"DB_PASSWORD=hunter22"}
	assert.Nil(t, service.maskSecretEnvInternal(t.Context(), map[string]string{}, env))
	assert.Equal(t, []string{"DB_PASSWORD=hunter22"}, env)
}
