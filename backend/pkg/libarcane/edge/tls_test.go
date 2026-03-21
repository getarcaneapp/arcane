package edge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareManagerMTLSAssets(t *testing.T) {
	cfg := &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
	}

	require.NoError(t, PrepareManagerMTLSAssets(cfg))
	require.NotEmpty(t, cfg.EdgeMTLSCAFile)
	require.FileExists(t, cfg.EdgeMTLSCAFile)
}

func TestGenerateManagerClientMTLSAssets(t *testing.T) {
	cfg := &Config{
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
	}

	assets, err := GenerateManagerClientMTLSAssets(cfg, "env-123")
	require.NoError(t, err)
	require.NotNil(t, assets)
	require.Equal(t, "./arcane-edge-certs", assets.HostDirHint)
	require.Len(t, assets.Files, 3)
	require.Equal(t, "ca.crt", assets.Files[0].Name)
	require.Contains(t, assets.Files[0].Content, "BEGIN CERTIFICATE")
	require.Equal(t, "agent.key", assets.Files[2].Name)
	require.Contains(t, assets.Files[2].Content, "BEGIN EC PRIVATE KEY")

	keyBlock, _ := pem.Decode([]byte(assets.Files[2].Content))
	require.NotNil(t, keyBlock)
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, elliptic.P384(), privateKey.Curve)

	certBlock, _ := pem.Decode([]byte(assets.Files[1].Content))
	require.NotNil(t, certBlock)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	require.Equal(t, elliptic.P384(), publicKey.Curve)
}

func TestEnsureAgentMTLSAssets_RejectsPlainHTTPEnrollment(t *testing.T) {
	cfg := &Config{
		ManagerApiUrl:     "http://manager.example.com/api",
		AgentToken:        "valid-token",
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
	}

	err := EnsureAgentMTLSAssets(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MANAGER_API_URL to use https for certificate enrollment")
}

func TestEnsureAgentMTLSAssets_UsesDownloadedCAPathWhenPresent(t *testing.T) {
	assetsDir := t.TempDir()
	certPath := filepath.Join(assetsDir, generatedMTLSClientCertName)
	keyPath := filepath.Join(assetsDir, generatedMTLSClientKeyName)
	caPath := filepath.Join(assetsDir, generatedMTLSCACertFileName)

	require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
	require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o600))
	require.NoError(t, os.WriteFile(caPath, []byte("ca"), 0o644))

	cfg := &Config{
		ManagerApiUrl:     "https://manager.example.com/api",
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: assetsDir,
	}

	require.NoError(t, EnsureAgentMTLSAssets(context.Background(), cfg))
	require.NotEmpty(t, cfg.EdgeMTLSCertFile)
	require.NotEmpty(t, cfg.EdgeMTLSKeyFile)
	require.FileExists(t, cfg.EdgeMTLSCertFile)
	require.FileExists(t, cfg.EdgeMTLSKeyFile)
	require.Equal(t, caPath, cfg.EdgeMTLSCAFile)
}

func TestValidateGeneratedClientCertificateInternal_RejectsMismatchedKeyPair(t *testing.T) {
	assetsDir := t.TempDir()

	clientCertPath, _, err := ensureClientCertificateInternal(assetsDir, "env-123")
	require.NoError(t, err)

	replacementKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	replacementKeyDER, err := x509.MarshalECPrivateKey(replacementKey)
	require.NoError(t, err)

	clientKeyPath := filepath.Join(assetsDir, generatedClientMTLSSubdir, "env-123", generatedMTLSClientKeyName)
	err = writePEMFileInternal(clientKeyPath, "EC PRIVATE KEY", replacementKeyDER, 0o600)
	require.NoError(t, err)

	err = validateGeneratedClientCertificateInternal(clientCertPath, clientKeyPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match private key")
}
