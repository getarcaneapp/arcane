package settings

import (
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
