package settings

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFieldVisibleForSettingVisibility(t *testing.T) {
	_, byKey := settingsFields()
	visibleKeys := func(visibility SettingVisibility) []string {
		keys := []string{}
		for key, field := range byKey {
			if fieldVisibleForSettingVisibility(field, visibility) {
				keys = append(keys, key)
			}
		}
		return keys
	}

	publicKeys := visibleKeys(SettingVisibilityPublic)
	require.Contains(t, publicKeys, "authLocalEnabled")
	require.Contains(t, publicKeys, "oidcEnabled")
	require.Contains(t, publicKeys, "oidcAutoRedirectToProvider")
	require.Contains(t, publicKeys, "oidcProviderName")
	require.Contains(t, publicKeys, "oidcProviderLogoUrl")
	require.NotContains(t, publicKeys, "dockerHost")
	require.NotContains(t, publicKeys, "oidcClientId")
	require.NotContains(t, publicKeys, "enableGravatar")

	nonAdminKeys := visibleKeys(SettingVisibilityNonAdmin)
	require.Contains(t, nonAdminKeys, "dockerHost")
	require.Contains(t, nonAdminKeys, "oidcClientId")
	require.Contains(t, nonAdminKeys, "oidcIssuerUrl")
	require.Contains(t, nonAdminKeys, "oidcScopes")
	require.Contains(t, nonAdminKeys, "oidcGroupsClaim")
	require.Contains(t, nonAdminKeys, "oidcSkipTlsVerify")
	require.Contains(t, nonAdminKeys, "oidcMergeAccounts")
	require.Contains(t, nonAdminKeys, "enableGravatar")
	require.Contains(t, nonAdminKeys, "avatarMaxUploadSizeMb")
	require.Contains(t, nonAdminKeys, "baseServerUrl")
	require.Contains(t, nonAdminKeys, "defaultShell")
	require.NotContains(t, nonAdminKeys, "oidcClientSecret")

	require.Contains(t, visibleKeys(SettingVisibilityAll), "oidcClientSecret")
}

func TestSettingsFieldDefaultsPassValidation(t *testing.T) {
	fields, _ := settingsFields()
	for _, field := range fields {
		if field.isInternal {
			continue
		}
		require.NoError(t, validateValue(field, field.defaultValue), field.key)
	}
}

func TestAgentTunnelSettingsMetadata(t *testing.T) {
	_, byKey := settingsFields()
	tests := []struct {
		key string
		def string
		min float64
		max float64
	}{
		{key: "http2ReceiveWindowMb", def: "32", min: 1, max: 256},
		{key: "agentTunnelStreamWindowMb", def: "16", min: 1, max: 64},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			field, ok := byKey[tt.key]
			require.True(t, ok)
			require.Equal(t, tt.def, field.defaultValue)
			require.Equal(t, "integer", field.valueType)
			require.InDelta(t, tt.min, field.min.MustGet(), 0)
			require.InDelta(t, tt.max, field.max.MustGet(), 0)
			require.Equal(t, SettingScopeEnvironment, field.scope)
			require.NoError(t, validateValue(field, strconv.FormatFloat(tt.min, 'f', -1, 64)))
			require.NoError(t, validateValue(field, strconv.FormatFloat(tt.max, 'f', -1, 64)))
			require.Error(t, validateValue(field, strconv.FormatFloat(tt.min-1, 'f', -1, 64)))
			require.Error(t, validateValue(field, strconv.FormatFloat(tt.max+1, 'f', -1, 64)))
			require.Error(t, validateValue(field, "1.5"))
		})
	}
}
