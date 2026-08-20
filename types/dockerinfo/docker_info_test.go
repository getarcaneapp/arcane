package dockerinfo

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInfoMarshalsEmbeddedDockerFieldsAtTopLevel(t *testing.T) {
	data, err := json.Marshal(Info{
		Name:              "arcane-host",
		NCPU:              8,
		MemTotal:          16 * 1024 * 1024 * 1024,
		ContainersRunning: 3,
		Success:           true,
		APIVersion:        "1.55",
	})

	require.NoError(t, err,
		"marshal Docker info: %v", err)

	var payload map[string]any
	{
		err := json.Unmarshal(data, &payload)
		require.NoError(t, err,
			"unmarshal Docker info payload: %v", err)
	}

	for name, want := range map[string]any{
		"Name":              "arcane-host",
		"NCPU":              float64(8),
		"ContainersRunning": float64(3),
		"apiVersion":        "1.55",
	} {
		{
			got := payload[name]
			require.Equal(t, want, got,
				"payload[%q] = %#v, want %#v; payload: %s", name, got, want, data)
		}

	}
	{
		_, nested := payload["Info"]
		require.False(t, nested,
			"Docker fields were nested under Info: %s", data)
	}

}
