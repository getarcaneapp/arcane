package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettings_ToSettingVariableSlice_Visibility(t *testing.T) {
	settings := &Settings{
		AuthLocalEnabled:           SettingVariable{Value: "true"},
		OidcEnabled:                SettingVariable{Value: "true"},
		OidcAutoRedirectToProvider: SettingVariable{Value: "false"},
		OidcProviderName:           SettingVariable{Value: "Pocket ID"},
		OidcProviderLogoUrl:        SettingVariable{Value: "https://id.ofkm.us/logo.png"},
		DockerHost:                 SettingVariable{Value: "unix:///var/run/docker.sock"},
		OidcClientId:               SettingVariable{Value: "client-id"},
		OidcIssuerUrl:              SettingVariable{Value: "https://issuer.example"},
		OidcScopes:                 SettingVariable{Value: "openid email profile"},
		OidcGroupsClaim:            SettingVariable{Value: "groups"},
		OidcSkipTlsVerify:          SettingVariable{Value: "false"},
		OidcMergeAccounts:          SettingVariable{Value: "true"},
		EnableGravatar:             SettingVariable{Value: "true"},
		AvatarMaxUploadSizeMb:      SettingVariable{Value: "2"},
		OidcClientSecret:           SettingVariable{Value: "secret"},
	}

	publicKeys := settingKeysFromSliceInternal(settings.ToSettingVariableSlice(SettingVisibilityPublic, true))
	require.Contains(t, publicKeys, "authLocalEnabled")
	require.Contains(t, publicKeys, "oidcEnabled")
	require.Contains(t, publicKeys, "oidcAutoRedirectToProvider")
	require.Contains(t, publicKeys, "oidcProviderName")
	require.Contains(t, publicKeys, "oidcProviderLogoUrl")
	require.NotContains(t, publicKeys, "dockerHost")
	require.NotContains(t, publicKeys, "oidcClientId")
	require.NotContains(t, publicKeys, "enableGravatar")

	nonAdminKeys := settingKeysFromSliceInternal(settings.ToSettingVariableSlice(SettingVisibilityNonAdmin, true))
	require.Contains(t, nonAdminKeys, "dockerHost")
	require.Contains(t, nonAdminKeys, "oidcClientId")
	require.Contains(t, nonAdminKeys, "oidcIssuerUrl")
	require.Contains(t, nonAdminKeys, "oidcScopes")
	require.Contains(t, nonAdminKeys, "oidcGroupsClaim")
	require.Contains(t, nonAdminKeys, "oidcSkipTlsVerify")
	require.Contains(t, nonAdminKeys, "oidcMergeAccounts")
	require.Contains(t, nonAdminKeys, "enableGravatar")
	require.Contains(t, nonAdminKeys, "avatarMaxUploadSizeMb")
	require.NotContains(t, nonAdminKeys, "baseServerUrl")
	require.NotContains(t, nonAdminKeys, "defaultShell")
	require.NotContains(t, nonAdminKeys, "oidcClientSecret")

	allKeys := settingKeysFromSliceInternal(settings.ToSettingVariableSlice(SettingVisibilityAll, true))
	require.Contains(t, allKeys, "oidcClientSecret")
}

func settingKeysFromSliceInternal(settings []SettingVariable) map[string]string {
	keys := make(map[string]string, len(settings))
	for _, setting := range settings {
		keys[setting.Key] = setting.Value
	}

	return keys
}
