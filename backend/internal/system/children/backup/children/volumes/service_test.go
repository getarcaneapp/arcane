package volumes

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/stretchr/testify/require"
)

func TestSystemVolumeBackupSelection(t *testing.T) {
	options := []backup.SystemVolumeBackupOption{
		{Name: "app", Available: true},
		{Name: "cache", Available: true},
		{Name: "anonymous", Anonymous: true, Available: true},
		{Name: "deleted", Available: false},
	}
	tests := []struct {
		name     string
		config   backup.SystemVolumeBackupPolicy
		expected []string
	}{
		{
			name:     "all includes future named volumes",
			config:   backup.SystemVolumeBackupPolicy{SelectionMode: backup.SystemVolumeSelectionAll, IgnoreAnonymous: true},
			expected: []string{"app", "cache"},
		},
		{
			name:     "allowlist includes exact live names only",
			config:   backup.SystemVolumeBackupPolicy{SelectionMode: backup.SystemVolumeSelectionAllowlist, VolumeNames: []string{"app", "deleted"}},
			expected: []string{"app"},
		},
		{
			name:     "blocklist includes unselected future and anonymous volumes",
			config:   backup.SystemVolumeBackupPolicy{SelectionMode: backup.SystemVolumeSelectionBlocklist, VolumeNames: []string{"cache"}},
			expected: []string{"app", "anonymous"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected := selectSystemVolumeBackupCandidates(tt.config, options)
			names := make([]string, len(selected))
			for i := range selected {
				names[i] = selected[i].Name
			}
			require.Equal(t, tt.expected, names)
		})
	}
}
