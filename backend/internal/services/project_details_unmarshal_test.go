package services

import (
	json "encoding/json/v2"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
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
			var details projecttypes.Details
			err := json.Unmarshal([]byte(test.payload), &details)
			if err != nil {
				t.Fatalf("unmarshal Details: %v", err)
			}
			if len(details.Services) != 1 {
				t.Fatalf("decoded %d services, want 1", len(details.Services))
			}
			if got, want := details.Services[0].MemLimit, composetypes.UnitBytes(256*1024*1024); got != want {
				t.Fatalf("mem_limit = %d, want %d", got, want)
			}
		})
	}
}
