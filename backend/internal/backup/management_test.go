package backup

import (
	"testing"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/stretchr/testify/require"
)

func TestParseManagementTypeFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		expected backuptypes.ManagementType
		filtered bool
	}{
		{name: "system", filter: " system ", expected: backuptypes.ManagementTypeSystem, filtered: true},
		{name: "volume", filter: "volume", expected: backuptypes.ManagementTypeVolume, filtered: true},
		{name: "duplicate", filter: "system,system", expected: backuptypes.ManagementTypeSystem, filtered: true},
		{name: "recognized and unknown", filter: "system,unknown", expected: backuptypes.ManagementTypeSystem, filtered: true},
		{name: "both", filter: "system,volume"},
		{name: "unknown", filter: "unknown"},
		{name: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, filtered := ParseManagementTypeFilter(tt.filter)
			require.Equal(t, tt.filtered, filtered)
			require.Equal(t, tt.expected, actual)
		})
	}
}
