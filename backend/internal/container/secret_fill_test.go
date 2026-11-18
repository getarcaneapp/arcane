package container

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/secretsource"
	"getarcane.dev/types/v3"
)

type fakeSecretFillerInternal struct {
	values map[string]string
}

func (f fakeSecretFillerInternal) ResolveTargets(context.Context, []types.SecretSourceTargetRef, types.Actor) (map[string]string, error) {
	return f.values, nil
}

func TestFillSecretEnvKeepsExplicitValues(t *testing.T) {
	service := &ContainerService{}
	service.SetSecretFiller(fakeSecretFillerInternal{values: map[string]string{"DB_PASSWORD": "from-source", "API_KEY": "k", "PORT": "9"}})

	env, keys, err := service.FillSecretEnv(t.Context(), []string{"PORT=8080", "MODE=prod"},
		[]types.SecretSourceTargetRef{{SourceID: "s1"}}, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, []string{"PORT=8080", "MODE=prod", "API_KEY=k", "DB_PASSWORD=from-source"}, env)
	assert.Equal(t, []string{"API_KEY", "DB_PASSWORD"}, keys, "a variable set in the form wins over the source")

	unchanged, none, err := service.FillSecretEnv(t.Context(), []string{"A=1"}, nil, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, []string{"A=1"}, unchanged)
	assert.Empty(t, none)

	_, _, err = (&ContainerService{}).FillSecretEnv(t.Context(), nil, []types.SecretSourceTargetRef{{SourceID: "s1"}}, types.Actor{})
	require.Error(t, err, "without secret sources wired in, a fill is refused")
}

func TestMaskSecretEnvUsesTheFillLabel(t *testing.T) {
	service := &ContainerService{}
	env := []string{"DB_PASSWORD=hunter22", "MODE=prod", "API_KEY=abc", "FLAG"}
	masked := service.maskSecretEnvInternal(t.Context(), map[string]string{SecretEnvKeysLabel: "DB_PASSWORD,API_KEY,FLAG"}, env)
	assert.Equal(t, []string{"DB_PASSWORD", "API_KEY"}, masked)
	assert.Equal(t, []string{"DB_PASSWORD=" + secretsource.RedactedMarker, "MODE=prod", "API_KEY=" + secretsource.RedactedMarker, "FLAG"}, env)

	assert.Nil(t, service.maskSecretEnvInternal(t.Context(), map[string]string{}, []string{"A=1"}))
}
