package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOidcConfig_MarshalDocument_PreservesOmitemptySemantics(t *testing.T) {
	config := OidcConfig{
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		IssuerURL:             "https://issuer.example",
		Scopes:                "openid email profile",
		AuthorizationEndpoint: "",
		TokenEndpoint:         "https://issuer.example/token",
		UserinfoEndpoint:      "",
		JwksURI:               "https://issuer.example/jwks",
		AdminClaim:            "",
		AdminValue:            "admins",
		SkipTlsVerify:         true,
	}

	data, err := config.MarshalDocument()
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))

	require.Equal(t, "client-id", doc["clientId"])
	require.Equal(t, "client-secret", doc["clientSecret"])
	require.Equal(t, "https://issuer.example", doc["issuerUrl"])
	require.Equal(t, "openid email profile", doc["scopes"])
	require.Equal(t, true, doc["skipTlsVerify"])

	require.NotContains(t, doc, "authorizationEndpoint")
	require.NotContains(t, doc, "userinfoEndpoint")
	require.NotContains(t, doc, "adminClaim")

	require.Equal(t, "https://issuer.example/token", doc["tokenEndpoint"])
	require.Equal(t, "https://issuer.example/jwks", doc["jwksUri"])
	require.Equal(t, "admins", doc["adminValue"])
}

func TestSettings_ToSettingVariableSlice_Visibility(t *testing.T) {
	settings := &Settings{
		ApplicationTheme:           SettingVariable{Value: "default"},
		AccentColor:                SettingVariable{Value: "oklch(0.6 0.2 240)"},
		OledMode:                   SettingVariable{Value: "false"},
		AuthLocalEnabled:           SettingVariable{Value: "true"},
		OidcEnabled:                SettingVariable{Value: "true"},
		OidcAutoRedirectToProvider: SettingVariable{Value: "false"},
		OidcProviderName:           SettingVariable{Value: "Pocket ID"},
		OidcProviderLogoUrl:        SettingVariable{Value: "https://id.ofkm.us/logo.png"},
		DockerHost:                 SettingVariable{Value: "unix:///var/run/docker.sock"},
		OidcClientId:               SettingVariable{Value: "client-id"},
		OidcIssuerUrl:              SettingVariable{Value: "https://issuer.example"},
		OidcScopes:                 SettingVariable{Value: "openid email profile"},
		OidcAdminClaim:             SettingVariable{Value: "groups"},
		OidcAdminValue:             SettingVariable{Value: "_arcane_admins"},
		OidcSkipTlsVerify:          SettingVariable{Value: "false"},
		OidcMergeAccounts:          SettingVariable{Value: "true"},
		MobileNavigationMode:       SettingVariable{Value: "floating"},
		MobileNavigationShowLabels: SettingVariable{Value: "true"},
		SidebarHoverExpansion:      SettingVariable{Value: "true"},
		KeyboardShortcutsEnabled:   SettingVariable{Value: "true"},
		OidcClientSecret:           SettingVariable{Value: "secret"},
	}

	publicKeys := settingKeysFromSliceInternal(settings.ToSettingVariableSlice(SettingVisibilityPublic, true))
	require.Contains(t, publicKeys, "applicationTheme")
	require.Contains(t, publicKeys, "authLocalEnabled")
	require.Contains(t, publicKeys, "oidcEnabled")
	require.Contains(t, publicKeys, "oidcAutoRedirectToProvider")
	require.Contains(t, publicKeys, "oidcProviderName")
	require.Contains(t, publicKeys, "oidcProviderLogoUrl")
	require.NotContains(t, publicKeys, "dockerHost")
	require.NotContains(t, publicKeys, "oidcClientId")
	require.NotContains(t, publicKeys, "mobileNavigationMode")

	nonAdminKeys := settingKeysFromSliceInternal(settings.ToSettingVariableSlice(SettingVisibilityNonAdmin, true))
	require.Contains(t, nonAdminKeys, "applicationTheme")
	require.Contains(t, nonAdminKeys, "dockerHost")
	require.Contains(t, nonAdminKeys, "oidcClientId")
	require.Contains(t, nonAdminKeys, "oidcIssuerUrl")
	require.Contains(t, nonAdminKeys, "oidcScopes")
	require.Contains(t, nonAdminKeys, "oidcAdminClaim")
	require.Contains(t, nonAdminKeys, "oidcAdminValue")
	require.Contains(t, nonAdminKeys, "oidcSkipTlsVerify")
	require.Contains(t, nonAdminKeys, "oidcMergeAccounts")
	require.Contains(t, nonAdminKeys, "mobileNavigationMode")
	require.Contains(t, nonAdminKeys, "keyboardShortcutsEnabled")
	require.NotContains(t, nonAdminKeys, "enableGravatar")
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
