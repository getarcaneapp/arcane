package selector

import (
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	libupdater "go.getarcane.app/updater/pkg/labels"
)

func TestSelectArcaneContainerSummary(t *testing.T) {
	tests := []struct {
		name  string
		items []containertypes.Summary
		want  string
		found bool
	}{
		{
			name: "server beats agent",
			items: []containertypes.Summary{
				{ID: "agent", State: containertypes.StateRunning, Labels: map[string]string{libupdater.LabelArcaneAgent: "true"}},
				{ID: "server", State: containertypes.StateRunning, Labels: map[string]string{libupdater.LabelArcane: "true"}},
			},
			want:  "server",
			found: true,
		},
		{
			name: "upgrader container is skipped",
			items: []containertypes.Summary{
				{ID: "upgrader", State: containertypes.StateRunning, Labels: map[string]string{libupdater.LabelArcane: "true", UpgraderLabel: "true"}},
				{ID: "server", State: containertypes.StateRunning, Labels: map[string]string{libupdater.LabelArcane: "true"}},
			},
			want:  "server",
			found: true,
		},
		{
			name: "running server beats stopped server",
			items: []containertypes.Summary{
				{ID: "stopped-server", State: containertypes.StateExited, Labels: map[string]string{libupdater.LabelArcane: "true"}},
				{ID: "running-server", State: containertypes.StateRunning, Labels: map[string]string{libupdater.LabelArcane: "true"}},
			},
			want:  "running-server",
			found: true,
		},
		{
			name: "agent is fallback when no server exists",
			items: []containertypes.Summary{
				{ID: "unrelated", State: containertypes.StateRunning, Labels: map[string]string{"com.example": "true"}},
				{ID: "agent", State: containertypes.StateRunning, Labels: map[string]string{libupdater.LabelArcaneAgent: "true"}},
			},
			want:  "agent",
			found: true,
		},
		{
			name: "no candidates",
			items: []containertypes.Summary{
				{ID: "unrelated", State: containertypes.StateRunning, Labels: map[string]string{"com.example": "true"}},
			},
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := SelectArcaneContainerSummary(tt.items)
			require.Equal(t, tt.found, found)
			if tt.found {
				require.Equal(t, tt.want, got.ID)
			}
		})
	}
}
