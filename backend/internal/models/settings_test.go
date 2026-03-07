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
