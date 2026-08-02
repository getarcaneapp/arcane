package project

import (
	"encoding/json/v2"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/require"
)

func TestDetailsUnmarshalJSONAcceptsUnitBytesStringsAndNumbers(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "human readable string",
			payload: `{"services":[{"name":"app","mem_limit":"256m"}]}`,
		},
		{
			name:    "number",
			payload: `{"services":[{"name":"app","mem_limit":268435456}]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var details Details
			err := json.Unmarshal([]byte(test.payload), &details)

			require.NoError(t, err,
				"unmarshal Details: %v", err)

			require.Len(t, details.Services, 1,
				"decoded %d services, want 1", len(details.Services))
			{

				got, want := details.Services[0].MemLimit, composetypes.UnitBytes(256*1024*1024)
				require.Equal(t, want, got,
					"mem_limit = %d, want %d", got, want)
			}

		})
	}
}
