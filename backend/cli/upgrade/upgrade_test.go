package upgrade

import (
	"testing"

	updaterlabels "github.com/getarcaneapp/updater/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRecreatedArcaneLabelsInternal(t *testing.T) {
	tests := []struct {
		name       string
		labels     map[string]string
		want       map[string]string
		wantSource map[string]string
	}{
		{
			name: "legacy server gains current Arcane label",
			labels: map[string]string{
				updaterlabels.LabelArcaneLegacyServer: "true",
				updaterlabels.LabelUpdater:            "false",
				"com.example.unrelated":               "keep",
			},
			want: map[string]string{
				updaterlabels.LabelArcaneLegacyServer: "true",
				updaterlabels.LabelArcane:             "true",
				updaterlabels.LabelUpdater:            "false",
				"com.example.unrelated":               "keep",
			},
			wantSource: map[string]string{
				updaterlabels.LabelArcaneLegacyServer: "true",
				updaterlabels.LabelUpdater:            "false",
				"com.example.unrelated":               "keep",
			},
		},
		{
			name: "agent gains current Arcane label",
			labels: map[string]string{
				updaterlabels.LabelArcaneAgent: "true",
			},
			want: map[string]string{
				updaterlabels.LabelArcane:      "true",
				updaterlabels.LabelArcaneAgent: "true",
			},
			wantSource: map[string]string{
				updaterlabels.LabelArcaneAgent: "true",
			},
		},
		{
			name: "unrelated labels are preserved without Arcane label",
			labels: map[string]string{
				"com.example.unrelated": "keep",
			},
			want: map[string]string{
				"com.example.unrelated": "keep",
			},
			wantSource: map[string]string{
				"com.example.unrelated": "keep",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRecreatedArcaneLabelsInternal(tt.labels)

			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantSource, tt.labels)
		})
	}
}
