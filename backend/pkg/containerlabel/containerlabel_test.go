package containerlabel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValue(t *testing.T) {
	_, ok := Value(nil, AutoHealLabel)
	require.False(t, ok)
	value, ok := Value(map[string]string{"COM.GETARCANEAPP.ARCANE.AUTO-HEAL": "true"}, AutoHealLabel)
	require.True(t, ok, "label names match case-insensitively")
	require.Equal(t, "true", value)
	for range 50 {
		value, _ = Value(map[string]string{AutoHealLabel: "false", "COM.GETARCANEAPP.ARCANE.AUTO-HEAL": "true"}, AutoHealLabel)
		require.Equal(t, "false", value, "the canonical key wins over other spellings")
		value, _ = Value(map[string]string{"COM.GETARCANEAPP.ARCANE.AUTO-HEAL": "a", "com.getarcaneapp.arcane.Auto-Heal": "b"}, AutoHealLabel)
		require.Equal(t, "a", value, "conflicting spellings resolve deterministically")
	}
}

func TestIsEnabled(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "1": true, "yes": true, "false": false, "0": false, "": false, "maybe": false} {
		require.Equal(t, want, IsEnabled(map[string]string{AutoHealLabel: value}, AutoHealLabel), "value %q", value)
	}
	require.True(t, IsEnabled(map[string]string{AutoHealLabel: " ON "}, AutoHealLabel), "values are trimmed")
	require.True(t, IsEnabled(map[string]string{"COM.GETARCANEAPP.ARCANE.AUTO-HEAL": "true"}, AutoHealLabel), "label names match case-insensitively")
	require.False(t, IsEnabled(nil, AutoHealLabel))
	require.False(t, IsEnabled(map[string]string{"com.getarcaneapp.arcane.updater": "true"}, AutoHealLabel), "other labels do not opt in")
}
