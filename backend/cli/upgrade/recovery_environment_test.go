package upgrade

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyRecoveredEnvironment(t *testing.T) {
	result := applyRecoveredEnvironment([]string{
		"ARCANE_AGENT_TOKEN=fresh", "ARCANE_AGENT_TOKEN_FILE=/run/secrets/agent", "ARCANE_ENCRYPTION_KEY__FILE=/run/secrets/key", "KEEP=value",
	}, map[string]string{"ARCANE_AGENT_TOKEN": "recovered-agent", "ARCANE_ENCRYPTION_KEY": "recovered-key", "NEW_SETTING": "new"})

	require.Contains(t, result, "ARCANE_AGENT_TOKEN=recovered-agent")
	require.Contains(t, result, "ARCANE_ENCRYPTION_KEY=recovered-key")
	require.Contains(t, result, "NEW_SETTING=new")
	require.Contains(t, result, "KEEP=value")
	require.False(t, slices.ContainsFunc(result, func(value string) bool {
		return value == "ARCANE_AGENT_TOKEN_FILE=/run/secrets/agent" || value == "ARCANE_ENCRYPTION_KEY__FILE=/run/secrets/key"
	}))
}
