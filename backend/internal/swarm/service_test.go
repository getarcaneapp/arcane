package swarm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeSwarmSpec_AllowsEmptyObject(t *testing.T) {
	spec, err := decodeSwarmSpec(json.RawMessage(`{}`))
	require.NoError(t, err)
	require.NotNil(t, spec.Labels)
	require.Empty(t, spec.Labels)
}

func TestDecodeSwarmSpec_RejectsNull(t *testing.T) {
	_, err := decodeSwarmSpec(json.RawMessage(`null`))
	require.EqualError(t, err, "swarm spec is required")
}
