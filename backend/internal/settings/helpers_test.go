package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnv(t *testing.T) {
	t.Setenv("ARCANE_TRIVY_NETWORK", "\"arcane-external\"")
	t.Setenv("ARCANE_BASE_SERVER_URL", "")

	env, err := LoadEnv()
	require.NoError(t, err)
	require.Equal(t, "arcane-external", env["trivyNetwork"])
	require.NotContains(t, env, "baseServerUrl")
	require.Equal(t, "arcane-external", env.ValueOrDefault("trivyNetwork"))
	require.Equal(t, "http://localhost", env.ValueOrDefault("baseServerUrl"))
}

func TestLoadEnvJoinsInvalidValues(t *testing.T) {
	t.Setenv("ARCANE_ENABLE_GRAVATAR", "yes")
	t.Setenv("ARCANE_AUTO_HEAL_INTERVAL", "not a cron")
	t.Setenv("ARCANE_TRIVY_NETWORK", "arcane-external")

	env, err := LoadEnv()
	require.Nil(t, env)
	require.ErrorContains(t, err, "ARCANE_ENABLE_GRAVATAR")
	require.ErrorContains(t, err, "ARCANE_AUTO_HEAL_INTERVAL")
	require.NotContains(t, err.Error(), "ARCANE_TRIVY_NETWORK")
}
