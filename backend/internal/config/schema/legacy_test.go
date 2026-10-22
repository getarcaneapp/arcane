package schema

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenamedEnv(t *testing.T) {
	renamed := RenamedEnv()

	assert.Equal(t, "ARCANE_PORT", renamed["PORT"])
	assert.Equal(t, "ARCANE_ENCRYPTION_KEY", renamed["ENCRYPTION_KEY"])
	assert.Equal(t, "ARCANE_EDGE_MTLS_MODE", renamed["EDGE_MTLS_MODE"])
	assert.Equal(t, "ARCANE_APNS_RELAY_URL", renamed["APNS_RELAY_URL"])
	assert.Equal(t, "ARCANE_PROJECTS_DIRECTORY", renamed["PROJECTS_DIRECTORY"])
	assert.Equal(t, "ARCANE_OIDC_CLIENT_SECRET", renamed["OIDC_CLIENT_SECRET"])
	assert.Equal(t, "ARCANE_AUTO_LOGIN_USERNAME", renamed["AUTO_LOGIN_USERNAME"])
	assert.Equal(t, "ARCANE_AUTO_LOGIN_PASSWORD", renamed["AUTO_LOGIN_PASSWORD"])
	for _, unchanged := range []string{"PUID", "DOCKER_CONFIG", "DOCKER_HOST", "TZ", "HTTP_PROXY", "BACKUP_VOLUME_NAME", "IN_CONTAINER", "ANALYTICS_ENABLED", "UPDATE_CHECK_ENABLED", "JWT_SECRET"} {
		assert.NotContains(t, renamed, unchanged)
	}
}

func TestManifestEnvironmentTranslatesLegacyManifests(t *testing.T) {
	legacy := types.RecoveryManifest{
		FormatVersion: 2,
		Environment: map[string]string{
			"DATABASE_URL":              "file:/app/data/arcane.db",
			"ENCRYPTION_KEY":            "legacy-key",
			"PROJECTS_DIRECTORY":        "/legacy/projects",
			"ARCANE_PROJECTS_DIRECTORY": "/current/projects",
			"PUID":                      "1000",
		},
	}

	assert.Equal(t, map[string]string{
		"ARCANE_DATABASE_URL":       "file:/app/data/arcane.db",
		"ARCANE_ENCRYPTION_KEY":     "legacy-key",
		"PROJECTS_DIRECTORY":        "/legacy/projects",
		"ARCANE_PROJECTS_DIRECTORY": "/current/projects",
		"PUID":                      "1000",
	}, ManifestEnvironment(legacy))

	current := types.RecoveryManifest{
		FormatVersion: types.RecoveryManifestFormatVersion,
		Environment:   map[string]string{"PORT": "3552"},
	}
	assert.Equal(t, map[string]string{"PORT": "3552"}, ManifestEnvironment(current))
}

func TestCheckLegacyEnv(t *testing.T) {
	t.Setenv("PORT", "4000")
	t.Setenv("ENCRYPTION_KEY_FILE", "/run/secrets/key")
	t.Setenv("PROJECTS_DIRECTORY", "/projects")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("ARCANE_LOG_LEVEL", "info")

	err := CheckLegacyEnv(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "rename PORT to ARCANE_PORT")
	require.ErrorContains(t, err, "rename ENCRYPTION_KEY_FILE to ARCANE_ENCRYPTION_KEY_FILE")
	require.ErrorContains(t, err, "rename PROJECTS_DIRECTORY to ARCANE_PROJECTS_DIRECTORY")
	assert.NotContains(t, err.Error(), "LOG_LEVEL")
}

func TestCheckLegacyEnvWarnsForRetiredNames(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	t.Setenv("JWT_SECRET", "legacy")
	t.Setenv("ANALYTICS_DISABLED", "true")

	require.NoError(t, CheckLegacyEnv(t.Context()))
	assert.Contains(t, logs.String(), "env=JWT_SECRET")
	assert.Contains(t, logs.String(), "env=ANALYTICS_DISABLED")
}
