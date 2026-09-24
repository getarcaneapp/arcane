package imageref

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.getarcane.app/updater/labels"
)

func TestIsUpdateCheckDisabled(t *testing.T) {
	for value, want := range map[string]bool{"false": true, "0": true, "no": true, " OFF ": true, "true": false, "yes": false, "": false, "maybe": false} {
		require.Equal(t, want, IsUpdateCheckDisabled(map[string]string{UpdateCheckLabel: value}), "value %q", value)
	}
	require.True(t, IsUpdateCheckDisabled(map[string]string{"COM.GETARCANEAPP.ARCANE.UPDATE-CHECK": "false"}), "label names match case-insensitively")
	for range 50 {
		require.False(t, IsUpdateCheckDisabled(map[string]string{UpdateCheckLabel: "true", "COM.GETARCANEAPP.ARCANE.UPDATE-CHECK": "false", "Com.GetArcaneApp.Arcane.Update-Check": "false"}), "the canonical key wins over other spellings")
		require.True(t, IsUpdateCheckDisabled(map[string]string{"COM.GETARCANEAPP.ARCANE.UPDATE-CHECK": "false", "com.getarcaneapp.arcane.Update-Check": "true"}), "conflicting spellings resolve deterministically")
	}
	require.False(t, IsUpdateCheckDisabled(nil))
	require.False(t, IsUpdateCheckDisabled(map[string]string{labels.LabelUpdater: "false"}), "the updater label only governs installation")
}

func TestUpdatePolicyKeyTracksMonitoringNotInstallation(t *testing.T) {
	base := map[string]string{labels.LabelUpdateStrategy: "tag", labels.LabelUpdateConstraint: "3.x"}
	installExcluded := map[string]string{labels.LabelUpdateStrategy: "tag", labels.LabelUpdateConstraint: "3.x", labels.LabelUpdater: "false"}
	unmonitored := map[string]string{labels.LabelUpdateStrategy: "tag", labels.LabelUpdateConstraint: "3.x", UpdateCheckLabel: "false"}
	require.Equal(t, UpdatePolicyKey("example:3.1.0", base), UpdatePolicyKey("example:3.1.0", installExcluded), "changing installation eligibility keeps results current")
	require.NotEqual(t, UpdatePolicyKey("example:3.1.0", base), UpdatePolicyKey("example:3.1.0", unmonitored), "changing monitoring eligibility invalidates results")
	require.NotEqual(t, UpdatePolicyKey("example:3.1.0", base), UpdatePolicyKey("example:3.1.0", map[string]string{labels.LabelUpdateStrategy: "tag", labels.LabelUpdateConstraint: "4.x"}))
}
