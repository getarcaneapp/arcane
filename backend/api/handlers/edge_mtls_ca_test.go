package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/stretchr/testify/require"
	libcrypto "go.getarcane.app/sys/crypto"
)

func TestMain(m *testing.M) {
	libcrypto.InitEncryption(&libcrypto.Config{
		EncryptionKey: "test-encryption-key-for-edge-mtls-32bytes-min",
		Environment:   "test",
	})
	os.Exit(m.Run())
}

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
			EdgeMTLSMode:      edge.EdgeMTLSModeRequired,
			EdgeMTLSAssetsDir: t.TempDir(),
		}
		require.NoError(t, edge.PrepareManagerMTLSAssetsWithContext(context.Background(), &edge.Config{
			EdgeMTLSMode:      cfg.EdgeMTLSMode,
			EdgeMTLSAssetsDir: cfg.EdgeMTLSAssetsDir,
			AppURL:            cfg.GetAppURL(),
		}))

		path, err := generatedEdgeMTLSCAPathInternal(cfg)

		require.NoError(t, err)
		require.FileExists(t, path)

		pemBytes, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Contains(t, string(pemBytes), "BEGIN CERTIFICATE")
	})

	t.Run("does not generate missing Arcane-managed CA", func(t *testing.T) {
		cfg := &config.Config{
			EdgeMTLSMode:      edge.EdgeMTLSModeRequired,
			EdgeMTLSAssetsDir: t.TempDir(),
		}

		_, err := generatedEdgeMTLSCAPathInternal(cfg)

		require.Error(t, err)
		require.NoFileExists(t, filepath.Join(cfg.EdgeMTLSAssetsDir, "ca.crt"))
		require.NoFileExists(t, filepath.Join(cfg.EdgeMTLSAssetsDir, "ca.key"))
	})
}

func TestReadGeneratedEdgeMTLSCertificateInfo(t *testing.T) {
	cfg := &config.Config{
		EdgeMTLSMode:      edge.EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
	}

	_, err := edge.GenerateManagerClientMTLSAssetsWithContext(context.Background(), &edge.Config{
		EdgeMTLSMode:      cfg.EdgeMTLSMode,
		EdgeMTLSAssetsDir: cfg.EdgeMTLSAssetsDir,
		AppURL:            cfg.GetAppURL(),
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

func TestEdgeMTLSCertificateDaysRemaining(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	require.Equal(t, 2, edgeMTLSCertificateDaysRemainingInternal(now, now.Add(48*time.Hour)))
	require.Equal(t, 3, edgeMTLSCertificateDaysRemainingInternal(now, now.Add(48*time.Hour+time.Second)))
	require.Equal(t, 0, edgeMTLSCertificateDaysRemainingInternal(now, now))
	require.Equal(t, 0, edgeMTLSCertificateDaysRemainingInternal(now, now.Add(-time.Second)))
}
