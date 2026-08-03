package swarm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeAgentStatusLegacyJSONCompatibility(t *testing.T) {
	var status NodeAgentStatus
	{
		err := json.Unmarshal([]byte(`{"state":"connected","environmentId":"env-1","connected":true}`), &status)
		require.NoError(t, err,
			"unmarshal legacy status: %v", err)
	}

	require.Equal(t, NodeAgentStateConnected, status.State,
		"state = %q, want %q", status.State, NodeAgentStateConnected)

	require.False(t, status.EnvironmentID == nil || *status.EnvironmentID != "env-1",
		"environmentId = %v, want env-1", status.EnvironmentID)

	require.Nil(t, status.BindingKind,
		"bindingKind = %v, want nil", status.BindingKind)

	require.Empty(t, status.Candidates,
		"candidates = %v, want empty", status.Candidates)

}

func TestNodeAgentStatusAmbiguousJSON(t *testing.T) {
	status := NodeAgentStatus{
		State: NodeAgentStateAmbiguous,
		Candidates: []NodeAgentCandidate{{
			EnvironmentID:   "env-1",
			EnvironmentName: "worker-1",
			EnvironmentType: "edge",
		}},
	}

	encoded, err := json.Marshal(status)

	require.NoError(t, err,
		"marshal ambiguous status: %v", err)

	var actual, expected any
	{
		err := json.Unmarshal(encoded, &actual)
		require.NoError(t, err,
			"unmarshal actual JSON: %v", err)
	}
	{

		err := json.Unmarshal([]byte(`{"state":"ambiguous","candidates":[{"environmentId":"env-1","environmentName":"worker-1","environmentType":"edge"}]}`), &expected)
		require.NoError(t, err,
			"unmarshal expected JSON: %v", err)
	}

	require.True(t, reflect.DeepEqual(actual, expected),
		"JSON = %s, want ambiguous candidate payload", encoded)

}

func TestSwarmJoinEnvironmentResultDoesNotExposeToken(t *testing.T) {
	result := SwarmJoinEnvironmentResult{EnvironmentID: "env-1", State: SwarmJoinEnvironmentResultJoined}
	encoded, err := json.Marshal(result)

	require.NoError(t, err,
		"marshal join result: %v", err)

	require.False(t, string(encoded) == "" || strings.Contains(string(encoded), "token"),
		"join result unexpectedly exposes a token field: %s", encoded)

}
