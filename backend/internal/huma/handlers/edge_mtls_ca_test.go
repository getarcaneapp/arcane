package handlers

import (
	"os"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/stretchr/testify/require"
)

func TestGeneratedEdgeMTLSCAPath(t *testing.T) {
	t.Run("returns error when edge mTLS is disabled", func(t *testing.T) {
		cfg := &config.Config{EdgeMTLSMode: edge.EdgeMTLSModeDisabled}

		_, err := generatedEdgeMTLSCAPathInternal(cfg)

		require.Error(t, err)
	})

	t.Run("returns configured CA path when CA is externally managed", func(t *testing.T) {
		caPath := t.TempDir() + "/custom-ca.crt"
		require.NoError(t, os.WriteFile(caPath, []byte("pem"), 0o644))

		cfg := &config.Config{
			EdgeMTLSMode:   edge.EdgeMTLSModeRequired,
			EdgeMTLSCAFile: caPath,
		}

		path, err := generatedEdgeMTLSCAPathInternal(cfg)

		require.NoError(t, err)
		require.Equal(t, caPath, path)
	})

	t.Run("returns generated CA path when Arcane manages the CA", func(t *testing.T) {
		cfg := &config.Config{
			EdgeMTLSMode:         edge.EdgeMTLSModeRequired,
			EdgeMTLSAutoGenerate: true,
			EdgeMTLSAssetsDir:    t.TempDir(),
		}

		path, err := generatedEdgeMTLSCAPathInternal(cfg)

		require.NoError(t, err)
		require.FileExists(t, path)

		pemBytes, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Contains(t, string(pemBytes), "BEGIN CERTIFICATE")
	})
}

func TestReadGeneratedEdgeMTLSCertificateInfo(t *testing.T) {
	cfg := &config.Config{
		EdgeMTLSMode:         edge.EdgeMTLSModeRequired,
		EdgeMTLSAutoGenerate: true,
		EdgeMTLSAssetsDir:    t.TempDir(),
	}

	_, err := edge.GenerateManagerClientMTLSAssets(&edge.Config{
		EdgeMTLSMode:         cfg.EdgeMTLSMode,
		EdgeMTLSAutoGenerate: cfg.EdgeMTLSAutoGenerate,
		EdgeMTLSAssetsDir:    cfg.EdgeMTLSAssetsDir,
	}, "env-123", "Lab Server")
	require.NoError(t, err)

	info, err := readGeneratedEdgeMTLSCertificateInfoInternal(cfg, "env-123")

	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, info.CommonName)
	require.Equal(t, "Lab-Server-env-123", *info.CommonName)
	require.NotNil(t, info.ExpiresAt)
	require.True(t, info.ExpiresAt.After(time.Now().UTC()))
	require.NotNil(t, info.DaysRemaining)
	require.True(t, *info.DaysRemaining > 0)
	require.False(t, info.Expired)
	require.False(t, info.ExpiringSoon)
}
